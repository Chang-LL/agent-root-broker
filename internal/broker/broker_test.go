package broker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"hostctl/internal/agent"
	"hostctl/internal/approval"
	"hostctl/internal/config"
	"hostctl/internal/executor"
	"hostctl/internal/proc"
)

func newTestBroker() (*Broker, proc.Identity) {
	cfg := config.Default()
	cfg.RequestTTLSeconds = 2
	cfg.MessageLeaseTTLSeconds = 10
	cfg.SessionLeaseTTLSeconds = 10
	b := New(cfg)
	b.alive = func(proc.Identity) bool { return true }
	b.execute = func(command executor.Command, _ config.Config) executor.Result {
		return executor.Result{ExitCode: 0, Stdout: command.Argv[len(command.Argv)-1] + "\n"}
	}
	process := proc.Identity{PID: 4242, UID: 1001, StartTime: 99}
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.SessionStarted, SessionID: "session-a"})
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "session-a"})
	return b, process
}

func testCommand(text string) executor.Command {
	return executor.Command{Argv: []string{"/bin/echo", text}, CWD: "/", TimeoutSeconds: 5, Hash: "hash-" + text, Risks: []string{}}
}

type requestResult struct {
	value Execution
	err   *Error
}

type automaticProvider struct{}

func (automaticProvider) Decide(context.Context, approval.Request) (approval.Decision, error) {
	return approval.Decision{
		Outcome: approval.Approved, Scope: approval.CommandScope,
		Provider: "automatic-test", Principal: "test-rule",
	}, nil
}

type delayedProvider struct {
	started chan struct{}
	release chan struct{}
}

func (p delayedProvider) Decide(context.Context, approval.Request) (approval.Decision, error) {
	close(p.started)
	<-p.release
	return approval.Decision{
		Outcome: approval.Approved, Scope: approval.CommandScope,
		Provider: "delayed-test", Principal: "test-rule",
	}, nil
}

func startRequest(b *Broker, process proc.Identity, text string) <-chan requestResult {
	result := make(chan requestResult, 1)
	go func() {
		value, err := b.Request(context.Background(), process, process.UID, testCommand(text))
		result <- requestResult{value, err}
	}()
	return result
}

func waitPending(t *testing.T, b *Broker) PendingView {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if items := b.Pending(); len(items) > 0 {
			return items[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("request did not become pending")
	return PendingView{}
}

func TestCommandApproval(t *testing.T) {
	b, process := newTestBroker()
	result := startRequest(b, process, "hello")
	pending := waitPending(t, b)
	if err := b.Decide(pending.ID, "approved", "command", 1000); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || got.value.Stdout != "hello\n" || got.value.ApprovalScope != "command" {
		t.Fatalf("unexpected result: %+v, %v", got.value, got.err)
	}
	if len(b.Leases()) != 0 {
		t.Fatal("command approval created a lease")
	}
}

func TestMessageScopeEndsAtStop(t *testing.T) {
	b, process := newTestBroker()
	first := startRequest(b, process, "first")
	pending := waitPending(t, b)
	if err := b.Decide(pending.ID, "approved", "message", 1000); err != nil {
		t.Fatal(err)
	}
	if got := <-first; got.err != nil {
		t.Fatal(got.err)
	}
	automatic, err := b.Request(context.Background(), process, process.UID, testCommand("second"))
	if err != nil || automatic.ApprovalScope != "message" {
		t.Fatalf("message lease not reused: %+v, %v", automatic, err)
	}
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnEnded, SessionID: "session-a"})
	if len(b.Leases()) != 0 {
		t.Fatal("message lease survived stop")
	}
	_, requestErr := b.Request(context.Background(), process, process.UID, testCommand("third"))
	if requestErr == nil || requestErr.Code != "no_active_turn" {
		t.Fatalf("unexpected error after stop: %v", requestErr)
	}
}

func TestSessionScopeSurvivesNextPrompt(t *testing.T) {
	b, process := newTestBroker()
	first := startRequest(b, process, "first")
	pending := waitPending(t, b)
	if err := b.Decide(pending.ID, "approved", "session", 1000); err != nil {
		t.Fatal(err)
	}
	<-first
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnEnded, SessionID: "session-a"})
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "session-a"})
	result, err := b.Request(context.Background(), process, process.UID, testCommand("next"))
	if err != nil || result.ApprovalScope != "session" {
		t.Fatalf("session lease not reused: %+v, %v", result, err)
	}
}

func TestDenialAndCancellation(t *testing.T) {
	b, process := newTestBroker()
	denied := startRequest(b, process, "denied")
	pending := waitPending(t, b)
	if err := b.Decide(pending.ID, "denied", "command", 1000); err != nil {
		t.Fatal(err)
	}
	if got := <-denied; got.err == nil || got.err.Code != "denied" {
		t.Fatalf("unexpected denial: %v", got.err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan *Error, 1)
	go func() {
		_, err := b.Request(ctx, process, process.UID, testCommand("cancel"))
		result <- err
	}()
	waitPending(t, b)
	cancel()
	if err := <-result; err == nil || err.Code != "cancelled" {
		t.Fatalf("unexpected cancellation: %v", err)
	}
}

func TestCustomDecisionProviderDoesNotRequireInteractiveReviewer(t *testing.T) {
	cfg := config.Default()
	b := NewWithProvider(cfg, automaticProvider{})
	b.alive = func(proc.Identity) bool { return true }
	b.execute = func(command executor.Command, _ config.Config) executor.Result {
		return executor.Result{ExitCode: 0, Stdout: command.Argv[len(command.Argv)-1] + "\n"}
	}
	process := proc.Identity{PID: 8, UID: 9, StartTime: 10}
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.SessionStarted, SessionID: "custom"})
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "custom"})

	result, err := b.Request(context.Background(), process, process.UID, testCommand("automatic"))
	if err != nil || result.Stdout != "automatic\n" || result.ApprovalScope != approval.CommandScope {
		t.Fatalf("unexpected custom-provider result: %+v, %v", result, err)
	}
	if len(b.Pending()) != 0 {
		t.Fatal("non-interactive provider exposed a manual queue")
	}
	if reviewErr := b.Decide("missing", approval.Approved, approval.CommandScope, 1000); reviewErr == nil || reviewErr.Code != "review_unsupported" {
		t.Fatalf("unexpected review error: %v", reviewErr)
	}
}

func TestProviderCannotApproveAfterTurnEnds(t *testing.T) {
	provider := delayedProvider{started: make(chan struct{}), release: make(chan struct{})}
	b := NewWithProvider(config.Default(), provider)
	b.alive = func(proc.Identity) bool { return true }
	var executed atomic.Bool
	b.execute = func(executor.Command, config.Config) executor.Result {
		executed.Store(true)
		return executor.Result{}
	}
	process := proc.Identity{PID: 11, UID: 12, StartTime: 13}
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.SessionStarted, SessionID: "delayed"})
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "delayed"})
	result := startRequest(b, process, "late")
	<-provider.started
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnEnded, SessionID: "delayed"})
	close(provider.release)

	got := <-result
	if got.err == nil || got.err.Code != approval.Cancelled {
		t.Fatalf("unexpected result after turn ended: %+v, %v", got.value, got.err)
	}
	if executed.Load() {
		t.Fatal("provider approval executed after its turn ended")
	}
}

func TestLifecycleSequenceFailsClosed(t *testing.T) {
	b := New(config.Default())
	b.alive = func(proc.Identity) bool { return true }
	process := proc.Identity{PID: 31, UID: 32, StartTime: 33}

	if err := b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "sequence"}); err == nil || err.Code != "lifecycle_out_of_order" {
		t.Fatalf("turn before session returned %v", err)
	}
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.SessionStarted, SessionID: "sequence"})
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.SessionStarted, SessionID: "sequence"})
	if err := b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "sequence"}); err != nil {
		t.Fatal(err)
	}
	if err := b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "sequence"}); err == nil || err.Code != "lifecycle_ambiguous" {
		t.Fatalf("duplicate active turn returned %v", err)
	}
	if _, err := b.activeSessionLocked(process); err == nil || err.Code != "no_active_turn" {
		t.Fatalf("ambiguous turn remained active: %v", err)
	}

	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnEnded, SessionID: "sequence"})
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "sequence"})
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnEnded, SessionID: "sequence"})
	if _, err := b.activeSessionLocked(process); err == nil || err.Code != "no_active_turn" {
		t.Fatalf("delayed stop left a turn active: %v", err)
	}
	_ = b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.SessionEnded, SessionID: "sequence"})
	if err := b.HandleLifecycle(process, agent.LifecycleEvent{Kind: agent.TurnStarted, SessionID: "sequence"}); err == nil || err.Code != "lifecycle_out_of_order" {
		t.Fatalf("turn after session end returned %v", err)
	}
}
