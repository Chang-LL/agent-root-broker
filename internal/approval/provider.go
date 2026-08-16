package approval

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Chang-LL/rootbroker/internal/executor"
	"github.com/Chang-LL/rootbroker/internal/proc"
)

const (
	Approved  = "approved"
	Denied    = "denied"
	Expired   = "expired"
	Cancelled = "cancelled"

	CommandScope = "command"
	MessageScope = "message"
	SessionScope = "session"

	ManualProviderName = "manual"
)

// Request is the vendor-neutral input to a decision provider.
type Request struct {
	ID        string
	Process   proc.Identity
	SessionID string
	Turn      int
	AgentUID  uint32
	Command   executor.Command
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Decision struct {
	Outcome     string
	Scope       string
	Provider    string
	Principal   string
	ApproverUID uint32
}

func (d Decision) Validate() error {
	if d.Outcome != Approved && d.Outcome != Denied && d.Outcome != Expired && d.Outcome != Cancelled {
		return fmt.Errorf("unknown decision outcome %q", d.Outcome)
	}
	if d.Outcome == Denied && d.Scope != CommandScope {
		return fmt.Errorf("denied decisions must use command scope")
	}
	if d.Outcome != Approved {
		return nil
	}
	if d.Scope != CommandScope && d.Scope != MessageScope && d.Scope != SessionScope {
		return fmt.Errorf("unknown approval scope %q", d.Scope)
	}
	if d.Provider == "" || d.Principal == "" {
		return fmt.Errorf("approved decisions must identify their provider and principal")
	}
	return nil
}

// Provider decides one request. Implementations may wait for a human, apply a
// local policy, or delegate elsewhere; the broker treats the provider as a
// separate trust boundary and still owns leases and command execution.
type Provider interface {
	Decide(context.Context, Request) (Decision, error)
}

// Reviewer is implemented by interactive providers that expose pending work to
// an authenticated approval transport. Automated providers need not implement it.
type Reviewer interface {
	Pending() []PendingView
	Review(requestID, outcome, scope string, approverUID uint32) (Reviewed, *ReviewError)
}

type Reviewed struct {
	Request  Request
	Decision Decision
}

type ReviewError struct {
	Code    string
	Message string
}

func (e *ReviewError) Error() string { return e.Message }

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

func (r Request) View() PendingView {
	return PendingView{
		ID: r.ID, SessionID: r.SessionID, Turn: r.Turn, AgentUID: r.AgentUID,
		Process: r.Process.Key(), CreatedAt: floatSeconds(r.CreatedAt),
		ExpiresAt: floatSeconds(r.ExpiresAt), Command: r.Command,
	}
}

type pending struct {
	request  Request
	context  context.Context
	decision Decision
	done     chan struct{}
}

// ManualProvider is the default local-human provider. It queues requests until
// an authenticated reviewer resolves them or their context ends.
type ManualProvider struct {
	mu      sync.Mutex
	pending map[string]*pending
}

var (
	_ Provider = (*ManualProvider)(nil)
	_ Reviewer = (*ManualProvider)(nil)
)

func NewManualProvider() *ManualProvider {
	return &ManualProvider{pending: make(map[string]*pending)}
}

func (p *ManualProvider) Decide(ctx context.Context, request Request) (Decision, error) {
	item := &pending{request: request, context: ctx, done: make(chan struct{})}
	p.mu.Lock()
	if _, exists := p.pending[request.ID]; exists {
		p.mu.Unlock()
		return Decision{Outcome: Cancelled}, nil
	}
	p.pending[request.ID] = item
	p.mu.Unlock()

	select {
	case <-item.done:
	case <-ctx.Done():
		p.mu.Lock()
		if item.decision.Outcome == "" {
			outcome := Cancelled
			if ctx.Err() == context.DeadlineExceeded {
				outcome = Expired
			}
			p.finishLocked(item, Decision{Outcome: outcome})
		}
		p.mu.Unlock()
	}

	p.mu.Lock()
	delete(p.pending, request.ID)
	decision := item.decision
	p.mu.Unlock()
	return decision, nil
}

func (p *ManualProvider) Pending() []PendingView {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	items := make([]Request, 0, len(p.pending))
	for _, item := range p.pending {
		items = append(items, item.request)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	views := make([]PendingView, 0, len(items))
	for _, item := range items {
		views = append(views, item.View())
	}
	return views
}

func (p *ManualProvider) Review(requestID, outcome, scope string, approverUID uint32) (Reviewed, *ReviewError) {
	if outcome != Approved && outcome != Denied {
		return Reviewed{}, reviewError("invalid_decision", "decision must be approved or denied")
	}
	if scope != CommandScope && scope != MessageScope && scope != SessionScope {
		return Reviewed{}, reviewError("invalid_scope", "scope must be command, message, or session")
	}
	if outcome == Denied {
		scope = CommandScope
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	target := p.pending[requestID]
	if target == nil || target.decision.Outcome != "" {
		return Reviewed{}, reviewError("not_found", "pending request not found: %s", requestID)
	}
	decision := Decision{
		Outcome: outcome, Scope: scope, Provider: ManualProviderName,
		Principal: fmt.Sprintf("uid:%d", approverUID), ApproverUID: approverUID,
	}
	if outcome == Approved && (scope == MessageScope || scope == SessionScope) {
		for _, item := range p.pending {
			sameContext := item.request.Process == target.request.Process && item.request.SessionID == target.request.SessionID &&
				(scope == SessionScope || item.request.Turn == target.request.Turn)
			if sameContext {
				p.finishLocked(item, decision)
			}
		}
	} else {
		p.finishLocked(target, decision)
	}
	return Reviewed{Request: target.request, Decision: decision}, nil
}

func (p *ManualProvider) pruneLocked() {
	for _, item := range p.pending {
		if item.decision.Outcome != "" || item.context.Err() == nil {
			continue
		}
		outcome := Cancelled
		if item.context.Err() == context.DeadlineExceeded {
			outcome = Expired
		}
		p.finishLocked(item, Decision{Outcome: outcome})
	}
}

func (p *ManualProvider) finishLocked(item *pending, decision Decision) {
	if item.decision.Outcome != "" {
		return
	}
	item.decision = decision
	close(item.done)
}

func reviewError(code, format string, args ...any) *ReviewError {
	return &ReviewError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func floatSeconds(value time.Time) float64 {
	return float64(value.UnixNano()) / float64(time.Second)
}
