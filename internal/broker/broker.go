package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"hostctl/internal/agent"
	"hostctl/internal/approval"
	"hostctl/internal/config"
	"hostctl/internal/executor"
	"hostctl/internal/proc"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func brokerError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

type Session struct {
	Process   proc.Identity
	SessionID string
	Turn      int
	Active    bool
	UpdatedAt time.Time
}

type PendingView = approval.PendingView

type Lease struct {
	ID               string
	Scope            string
	Process          proc.Identity
	SessionID        string
	Turn             *int
	DecisionProvider string
	Principal        string
	ApproverUID      uint32
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

type LeaseView struct {
	ID               string  `json:"id"`
	Scope            string  `json:"scope"`
	Process          string  `json:"process"`
	SessionID        string  `json:"sessionId"`
	Turn             *int    `json:"turn"`
	DecisionProvider string  `json:"decisionProvider"`
	Principal        string  `json:"principal"`
	ApproverUID      uint32  `json:"approverUid"`
	CreatedAt        float64 `json:"createdAt"`
	ExpiresAt        float64 `json:"expiresAt"`
}

func (l *Lease) View() LeaseView {
	return LeaseView{
		ID: l.ID, Scope: l.Scope, Process: l.Process.Key(), SessionID: l.SessionID,
		Turn: l.Turn, DecisionProvider: l.DecisionProvider, Principal: l.Principal, ApproverUID: l.ApproverUID,
		CreatedAt: floatSeconds(l.CreatedAt), ExpiresAt: floatSeconds(l.ExpiresAt),
	}
}

type Execution struct {
	OK            bool   `json:"ok"`
	RequestID     any    `json:"requestId"`
	ApprovalScope string `json:"approvalScope"`
	CommandHash   string `json:"commandHash"`
	executor.Result
}

type inflightRequest struct {
	Process   proc.Identity
	SessionID string
	ExpiresAt time.Time
	Cancel    context.CancelFunc
}

type Broker struct {
	cfg      config.Config
	mu       sync.Mutex
	sessions map[string]*Session
	inflight map[string]inflightRequest
	leases   map[string]*Lease
	provider approval.Provider
	reviewer approval.Reviewer
	now      func() time.Time
	alive    func(proc.Identity) bool
	execute  func(executor.Command, config.Config) executor.Result
}

func New(cfg config.Config) *Broker {
	return NewWithProvider(cfg, approval.NewManualProvider())
}

func NewWithProvider(cfg config.Config, provider approval.Provider) *Broker {
	if provider == nil {
		provider = approval.NewManualProvider()
	}
	reviewer, _ := provider.(approval.Reviewer)
	return &Broker{
		cfg: cfg, sessions: make(map[string]*Session), inflight: make(map[string]inflightRequest),
		leases: make(map[string]*Lease), provider: provider, reviewer: reviewer,
		now: time.Now, alive: proc.Alive, execute: executor.Execute,
	}
}

func sessionKey(process proc.Identity, sessionID string) string {
	return process.Key() + "\x00" + sessionID
}

func floatSeconds(value time.Time) float64 {
	return float64(value.UnixNano()) / float64(time.Second)
}

func (b *Broker) HandleLifecycle(process proc.Identity, event agent.LifecycleEvent) *Error {
	if err := event.Validate(); err != nil {
		return brokerError("invalid_lifecycle", "invalid lifecycle event: %v", err)
	}
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	key := sessionKey(process, event.SessionID)
	switch event.Kind {
	case agent.SessionStarted:
		b.sessions[key] = &Session{Process: process, SessionID: event.SessionID, UpdatedAt: now}
	case agent.TurnStarted:
		state := b.sessions[key]
		if state == nil {
			state = &Session{Process: process, SessionID: event.SessionID}
			b.sessions[key] = state
		}
		b.cancelPendingLocked(process, event.SessionID, "new-turn")
		state.Turn++
		state.Active = true
		state.UpdatedAt = now
		b.revokeMessageLeasesLocked(process, event.SessionID)
	case agent.TurnEnded:
		state := b.sessions[key]
		if state != nil {
			state.Active = false
			state.UpdatedAt = now
			b.revokeMessageLeasesLocked(process, event.SessionID)
			b.cancelPendingLocked(process, event.SessionID, "turn-ended")
		}
	case agent.SessionEnded:
		b.removeSessionLocked(process, event.SessionID, "session-end")
	}
	b.audit("lifecycle", map[string]any{"kind": event.Kind, "session": event.SessionID, "process": process.Key()})
	return nil
}

func (b *Broker) Request(ctx context.Context, process proc.Identity, agentUID uint32, command executor.Command) (Execution, *Error) {
	now := b.now()
	b.mu.Lock()
	b.pruneLocked(now)
	session, err := b.activeSessionLocked(process)
	if err != nil {
		b.mu.Unlock()
		return Execution{}, err
	}
	var requestID any
	var approvalScope string
	var decisionProvider string
	var decisionPrincipal string
	var approverUID uint32
	if lease := b.matchingLeaseLocked(session, now); lease != nil {
		requestID = nil
		approvalScope = lease.Scope
		decisionProvider = lease.DecisionProvider
		decisionPrincipal = lease.Principal
		approverUID = lease.ApproverUID
		b.mu.Unlock()
	} else {
		id, idErr := randomID()
		if idErr != nil {
			b.mu.Unlock()
			return Execution{}, brokerError("internal_error", "create request ID: %v", idErr)
		}
		request := approval.Request{
			ID: id, Process: process, SessionID: session.SessionID, Turn: session.Turn,
			AgentUID: agentUID, Command: command, CreatedAt: now,
			ExpiresAt: now.Add(time.Duration(b.cfg.RequestTTLSeconds) * time.Second),
		}
		decisionContext, cancel := context.WithDeadline(ctx, request.ExpiresAt)
		b.inflight[id] = inflightRequest{
			Process: process, SessionID: session.SessionID, ExpiresAt: request.ExpiresAt, Cancel: cancel,
		}
		fields := map[string]any{
			"request": id, "command_hash": command.Hash, "executable": command.Argv[0],
			"session": session.SessionID, "turn": session.Turn,
		}
		if b.cfg.LogArgv {
			fields["argv"] = command.Argv
		}
		b.audit("request-created", fields)
		b.mu.Unlock()

		decision, providerErr := b.provider.Decide(decisionContext, request)
		cancel()
		b.mu.Lock()
		delete(b.inflight, id)
		if providerErr != nil {
			b.mu.Unlock()
			return Execution{}, brokerError("decision_provider_failed", "decision provider failed: %v", providerErr)
		}
		if err := decision.Validate(); err != nil {
			b.mu.Unlock()
			return Execution{}, brokerError("invalid_provider_decision", "decision provider returned an invalid result: %v", err)
		}
		if decision.Outcome == approval.Approved {
			if !b.requestContextActiveLocked(request) {
				b.mu.Unlock()
				return Execution{}, brokerError("cancelled", "request %s no longer belongs to the active agent turn", id)
			}
			if err := b.ensureLeaseLocked(request, decision, b.now()); err != nil {
				b.mu.Unlock()
				return Execution{}, err
			}
		}
		b.mu.Unlock()
		if decision.Outcome != approval.Approved {
			return Execution{}, brokerError(decision.Outcome, "request %s was %s", id, decision.Outcome)
		}
		requestID, approvalScope = id, decision.Scope
		decisionProvider, decisionPrincipal, approverUID = decision.Provider, decision.Principal, decision.ApproverUID
	}

	if ctx.Err() != nil || !b.alive(process) {
		return Execution{}, brokerError("cancelled", "agent disconnected or exited before execution")
	}
	b.audit("execution-started", map[string]any{
		"request": requestID, "command_hash": command.Hash,
		"scope": approvalScope, "decision_provider": decisionProvider,
		"principal": decisionPrincipal, "approver_uid": approverUID,
	})
	result := b.execute(command, b.cfg)
	b.audit("execution-finished", map[string]any{
		"request": requestID, "command_hash": command.Hash,
		"exit_code": result.ExitCode, "duration_ms": result.DurationMS,
	})
	return Execution{
		OK: true, RequestID: requestID, ApprovalScope: approvalScope,
		CommandHash: command.Hash, Result: result,
	}, nil
}

func (b *Broker) Pending() []PendingView {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(b.now())
	if b.reviewer == nil {
		return []PendingView{}
	}
	return b.reviewer.Pending()
}

func (b *Broker) Leases() []LeaseView {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(b.now())
	items := make([]*Lease, 0, len(b.leases))
	for _, item := range b.leases {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	views := make([]LeaseView, 0, len(items))
	for _, item := range items {
		views = append(views, item.View())
	}
	return views
}

func (b *Broker) Decide(requestID, decision, scope string, approverUID uint32) *Error {
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	if b.reviewer == nil {
		return brokerError("review_unsupported", "the configured decision provider does not accept interactive reviews")
	}
	reviewed, reviewErr := b.reviewer.Review(requestID, decision, scope, approverUID)
	if reviewErr != nil {
		return brokerError(reviewErr.Code, "%s", reviewErr.Message)
	}
	if decision == approval.Denied {
		scope = approval.CommandScope
	}
	if err := reviewed.Decision.Validate(); err != nil ||
		reviewed.Decision.Outcome != decision || reviewed.Decision.Scope != scope {
		return brokerError("invalid_provider_decision", "interactive decision provider returned an invalid or mismatched review")
	}
	if decision == approval.Approved {
		if err := b.ensureLeaseLocked(reviewed.Request, reviewed.Decision, now); err != nil {
			return err
		}
	}
	b.audit("request-decided", map[string]any{
		"request": requestID, "decision": decision, "scope": scope,
		"decision_provider": reviewed.Decision.Provider, "principal": reviewed.Decision.Principal,
		"approver_uid": approverUID, "command_hash": reviewed.Request.Command.Hash,
	})
	return nil
}

func (b *Broker) Revoke(leaseID string, approverUID uint32) *Error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.leases[leaseID]; !ok {
		return brokerError("not_found", "lease not found: %s", leaseID)
	}
	delete(b.leases, leaseID)
	b.audit("lease-revoked", map[string]any{"lease": leaseID, "approver_uid": approverUID})
	return nil
}

func (b *Broker) activeSessionLocked(process proc.Identity) (*Session, *Error) {
	var found *Session
	for _, session := range b.sessions {
		if session.Process == process && session.Active {
			if found != nil {
				return nil, brokerError("no_active_turn", "no unique active agent turn")
			}
			found = session
		}
	}
	if found == nil {
		return nil, brokerError("no_active_turn", "no unique active agent turn; use the integration's managed launcher and submit a prompt")
	}
	return found, nil
}

func (b *Broker) matchingLeaseLocked(session *Session, now time.Time) *Lease {
	for _, lease := range b.leases {
		if !lease.ExpiresAt.After(now) || lease.Process != session.Process || lease.SessionID != session.SessionID {
			continue
		}
		if lease.Scope == "session" || (lease.Scope == "message" && lease.Turn != nil && *lease.Turn == session.Turn) {
			return lease
		}
	}
	return nil
}

func (b *Broker) requestContextActiveLocked(request approval.Request) bool {
	session := b.sessions[sessionKey(request.Process, request.SessionID)]
	return session != nil && session.Active && session.Turn == request.Turn
}

func (b *Broker) ensureLeaseLocked(request approval.Request, decision approval.Decision, now time.Time) *Error {
	if decision.Scope == approval.CommandScope {
		return nil
	}
	for _, lease := range b.leases {
		if !lease.ExpiresAt.After(now) || lease.Process != request.Process || lease.SessionID != request.SessionID {
			continue
		}
		if lease.Scope == approval.SessionScope ||
			(decision.Scope == approval.MessageScope && lease.Scope == approval.MessageScope && lease.Turn != nil && *lease.Turn == request.Turn) {
			return nil
		}
	}
	ttl := b.cfg.SessionLeaseTTLSeconds
	var turn *int
	if decision.Scope == approval.MessageScope {
		ttl = b.cfg.MessageLeaseTTLSeconds
		value := request.Turn
		turn = &value
	}
	leaseID, err := randomID()
	if err != nil {
		return brokerError("internal_error", "create lease ID: %v", err)
	}
	b.leases[leaseID] = &Lease{
		ID: leaseID, Scope: decision.Scope, Process: request.Process, SessionID: request.SessionID,
		Turn: turn, DecisionProvider: decision.Provider, Principal: decision.Principal,
		ApproverUID: decision.ApproverUID, CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(ttl) * time.Second),
	}
	return nil
}

func (b *Broker) revokeMessageLeasesLocked(process proc.Identity, sessionID string) {
	for id, lease := range b.leases {
		if lease.Scope == "message" && lease.Process == process && lease.SessionID == sessionID {
			delete(b.leases, id)
		}
	}
}

func (b *Broker) cancelPendingLocked(process proc.Identity, sessionID, reason string) {
	for id, request := range b.inflight {
		if request.Process == process && request.SessionID == sessionID {
			request.Cancel()
			b.audit("request-cancelled", map[string]any{"request": id, "reason": reason})
		}
	}
}

func (b *Broker) removeSessionLocked(process proc.Identity, sessionID, reason string) {
	delete(b.sessions, sessionKey(process, sessionID))
	for id, lease := range b.leases {
		if lease.Process == process && lease.SessionID == sessionID {
			delete(b.leases, id)
		}
	}
	b.cancelPendingLocked(process, sessionID, reason)
	b.audit("session-removed", map[string]any{"session": sessionID, "process": process.Key(), "reason": reason})
}

func (b *Broker) pruneLocked(now time.Time) {
	for id, lease := range b.leases {
		if !lease.ExpiresAt.After(now) || !b.alive(lease.Process) {
			delete(b.leases, id)
		}
	}
	for key, session := range b.sessions {
		if !b.alive(session.Process) {
			delete(b.sessions, key)
			for id, lease := range b.leases {
				if lease.Process == session.Process && lease.SessionID == session.SessionID {
					delete(b.leases, id)
				}
			}
			b.cancelPendingLocked(session.Process, session.SessionID, "process-exited")
		}
	}
	for _, request := range b.inflight {
		if !request.ExpiresAt.After(now) {
			request.Cancel()
		}
	}
}

func (b *Broker) audit(event string, fields map[string]any) {
	payload := make(map[string]any, len(fields)+1)
	payload["event"] = event
	for key, value := range fields {
		payload[key] = value
	}
	encoded, _ := json.Marshal(payload)
	log.Printf("hostctl.audit %s", encoded)
}

func randomID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
