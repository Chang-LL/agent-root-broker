package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

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

type Pending struct {
	ID          string
	Process     proc.Identity
	SessionID   string
	Turn        int
	AgentUID    uint32
	Command     executor.Command
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Decision    string
	Scope       string
	ApproverUID uint32
	done        chan struct{}
}

type PendingView struct {
	ID        string           `json:"id"`
	SessionID string           `json:"sessionId"`
	Turn      int              `json:"turn"`
	AgentUID  uint32           `json:"agentUid"`
	Process   string           `json:"process"`
	CreatedAt float64          `json:"createdAt"`
	ExpiresAt float64          `json:"expiresAt"`
	Decision  *string          `json:"decision"`
	Scope     *string          `json:"scope"`
	Command   executor.Command `json:"command"`
}

func (p *Pending) View() PendingView {
	view := PendingView{
		ID: p.ID, SessionID: p.SessionID, Turn: p.Turn, AgentUID: p.AgentUID,
		Process: p.Process.Key(), CreatedAt: floatSeconds(p.CreatedAt),
		ExpiresAt: floatSeconds(p.ExpiresAt), Command: p.Command,
	}
	if p.Decision != "" {
		decision := p.Decision
		view.Decision = &decision
	}
	if p.Scope != "" {
		scope := p.Scope
		view.Scope = &scope
	}
	return view
}

type Lease struct {
	ID          string
	Scope       string
	Process     proc.Identity
	SessionID   string
	Turn        *int
	ApproverUID uint32
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type LeaseView struct {
	ID          string  `json:"id"`
	Scope       string  `json:"scope"`
	Process     string  `json:"process"`
	SessionID   string  `json:"sessionId"`
	Turn        *int    `json:"turn"`
	ApproverUID uint32  `json:"approverUid"`
	CreatedAt   float64 `json:"createdAt"`
	ExpiresAt   float64 `json:"expiresAt"`
}

func (l *Lease) View() LeaseView {
	return LeaseView{
		ID: l.ID, Scope: l.Scope, Process: l.Process.Key(), SessionID: l.SessionID,
		Turn: l.Turn, ApproverUID: l.ApproverUID,
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

type Broker struct {
	cfg      config.Config
	mu       sync.Mutex
	sessions map[string]*Session
	pending  map[string]*Pending
	leases   map[string]*Lease
	now      func() time.Time
	alive    func(proc.Identity) bool
	execute  func(executor.Command, config.Config) executor.Result
}

func New(cfg config.Config) *Broker {
	return &Broker{
		cfg: cfg, sessions: make(map[string]*Session), pending: make(map[string]*Pending),
		leases: make(map[string]*Lease), now: time.Now, alive: proc.Alive, execute: executor.Execute,
	}
}

func sessionKey(process proc.Identity, sessionID string) string {
	return process.Key() + "\x00" + sessionID
}

func floatSeconds(value time.Time) float64 {
	return float64(value.UnixNano()) / float64(time.Second)
}

func eventName(event map[string]any) string {
	name, _ := event["hookEventName"].(string)
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

func (b *Broker) HandleHook(process proc.Identity, event map[string]any) *Error {
	sessionID, _ := event["sessionId"].(string)
	if sessionID == "" || len(sessionID) > 256 {
		return brokerError("invalid_hook", "hook event is missing a valid sessionId")
	}
	name := eventName(event)
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	key := sessionKey(process, sessionID)
	switch name {
	case "sessionstart":
		b.sessions[key] = &Session{Process: process, SessionID: sessionID, UpdatedAt: now}
	case "userpromptsubmit":
		state := b.sessions[key]
		if state == nil {
			state = &Session{Process: process, SessionID: sessionID}
			b.sessions[key] = state
		}
		b.cancelPendingLocked(process, sessionID, "new-turn")
		state.Turn++
		state.Active = true
		state.UpdatedAt = now
		b.revokeMessageLeasesLocked(process, sessionID)
	case "stop", "stopfailure":
		state := b.sessions[key]
		reason, _ := event["reason"].(string)
		if state != nil && (name == "stopfailure" || reason == "end_turn") {
			state.Active = false
			state.UpdatedAt = now
			b.revokeMessageLeasesLocked(process, sessionID)
			b.cancelPendingLocked(process, sessionID, "turn-ended")
		}
	case "sessionend":
		b.removeSessionLocked(process, sessionID, "session-end")
	}
	b.audit("hook", map[string]any{"hook": name, "session": sessionID, "process": process.Key()})
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
	var approverUID uint32
	if lease := b.matchingLeaseLocked(session, now); lease != nil {
		requestID = nil
		approvalScope = lease.Scope
		approverUID = lease.ApproverUID
		b.mu.Unlock()
	} else {
		id, idErr := randomID()
		if idErr != nil {
			b.mu.Unlock()
			return Execution{}, brokerError("internal_error", "create request ID: %v", idErr)
		}
		pending := &Pending{
			ID: id, Process: process, SessionID: session.SessionID, Turn: session.Turn,
			AgentUID: agentUID, Command: command, CreatedAt: now,
			ExpiresAt: now.Add(time.Duration(b.cfg.RequestTTLSeconds) * time.Second), done: make(chan struct{}),
		}
		b.pending[id] = pending
		fields := map[string]any{
			"request": id, "command_hash": command.Hash, "executable": command.Argv[0],
			"session": session.SessionID, "turn": session.Turn,
		}
		if b.cfg.LogArgv {
			fields["argv"] = command.Argv
		}
		b.audit("request-created", fields)
		b.mu.Unlock()

		timer := time.NewTimer(time.Until(pending.ExpiresAt))
		select {
		case <-pending.done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			b.finishPending(id, "expired", "", 0)
		case <-ctx.Done():
			b.finishPending(id, "cancelled", "", 0)
		}

		b.mu.Lock()
		delete(b.pending, id)
		decision, scope, approvedBy := pending.Decision, pending.Scope, pending.ApproverUID
		b.mu.Unlock()
		if decision != "approved" {
			if decision == "" {
				decision = "cancelled"
			}
			return Execution{}, brokerError(decision, "request %s was %s", id, decision)
		}
		requestID, approvalScope, approverUID = id, scope, approvedBy
	}

	if ctx.Err() != nil || !b.alive(process) {
		return Execution{}, brokerError("cancelled", "agent disconnected or exited before execution")
	}
	b.audit("execution-started", map[string]any{
		"request": requestID, "command_hash": command.Hash,
		"scope": approvalScope, "approver_uid": approverUID,
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
	items := make([]*Pending, 0, len(b.pending))
	for _, item := range b.pending {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	views := make([]PendingView, 0, len(items))
	for _, item := range items {
		views = append(views, item.View())
	}
	return views
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
	if decision != "approved" && decision != "denied" {
		return brokerError("invalid_decision", "decision must be approved or denied")
	}
	if scope != "command" && scope != "message" && scope != "session" {
		return brokerError("invalid_scope", "scope must be command, message, or session")
	}
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	pending := b.pending[requestID]
	if pending == nil || pending.Decision != "" {
		return brokerError("not_found", "pending request not found: %s", requestID)
	}
	if decision == "denied" {
		scope = "command"
	}
	if decision == "approved" && (scope == "message" || scope == "session") {
		ttl := b.cfg.SessionLeaseTTLSeconds
		var turn *int
		if scope == "message" {
			ttl = b.cfg.MessageLeaseTTLSeconds
			value := pending.Turn
			turn = &value
		}
		leaseID, err := randomID()
		if err != nil {
			return brokerError("internal_error", "create lease ID: %v", err)
		}
		lease := &Lease{
			ID: leaseID, Scope: scope, Process: pending.Process, SessionID: pending.SessionID,
			Turn: turn, ApproverUID: approverUID, CreatedAt: now,
			ExpiresAt: now.Add(time.Duration(ttl) * time.Second),
		}
		b.leases[lease.ID] = lease
		for _, other := range b.pending {
			sameContext := other.Process == pending.Process && other.SessionID == pending.SessionID &&
				(scope == "session" || other.Turn == pending.Turn)
			if sameContext && other.Decision == "" {
				b.completePendingLocked(other, "approved", scope, approverUID)
			}
		}
	} else {
		b.completePendingLocked(pending, decision, scope, approverUID)
	}
	b.audit("request-decided", map[string]any{
		"request": requestID, "decision": decision, "scope": scope,
		"approver_uid": approverUID, "command_hash": pending.Command.Hash,
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
				return nil, brokerError("no_active_turn", "no unique active Grok turn")
			}
			found = session
		}
	}
	if found == nil {
		return nil, brokerError("no_active_turn", "no unique active Grok turn; start Grok through grok-safe and submit a prompt")
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

func (b *Broker) finishPending(id, decision, scope string, approverUID uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if pending := b.pending[id]; pending != nil {
		b.completePendingLocked(pending, decision, scope, approverUID)
	}
}

func (b *Broker) completePendingLocked(pending *Pending, decision, scope string, approverUID uint32) {
	if pending.Decision != "" {
		return
	}
	pending.Decision = decision
	pending.Scope = scope
	pending.ApproverUID = approverUID
	close(pending.done)
}

func (b *Broker) revokeMessageLeasesLocked(process proc.Identity, sessionID string) {
	for id, lease := range b.leases {
		if lease.Scope == "message" && lease.Process == process && lease.SessionID == sessionID {
			delete(b.leases, id)
		}
	}
}

func (b *Broker) cancelPendingLocked(process proc.Identity, sessionID, reason string) {
	for _, pending := range b.pending {
		if pending.Process == process && pending.SessionID == sessionID && pending.Decision == "" {
			b.completePendingLocked(pending, "cancelled", "", 0)
			b.audit("request-cancelled", map[string]any{"request": pending.ID, "reason": reason})
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
	for _, pending := range b.pending {
		if pending.Decision == "" && !pending.ExpiresAt.After(now) {
			b.completePendingLocked(pending, "expired", "", 0)
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
