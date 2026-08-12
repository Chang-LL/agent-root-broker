package broker

import (
	"context"
	"testing"
	"time"

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
	_ = b.HandleHook(process, map[string]any{"hookEventName": "session_start", "sessionId": "session-a"})
	_ = b.HandleHook(process, map[string]any{"hookEventName": "user_prompt_submit", "sessionId": "session-a"})
	return b, process
}

func testCommand(text string) executor.Command {
	return executor.Command{Argv: []string{"/bin/echo", text}, CWD: "/", TimeoutSeconds: 5, Hash: "hash-" + text, Risks: []string{}}
}

type requestResult struct {
	value Execution
	err   *Error
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
	_ = b.HandleHook(process, map[string]any{"hookEventName": "stop", "sessionId": "session-a", "reason": "end_turn"})
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
	_ = b.HandleHook(process, map[string]any{"hookEventName": "stop", "sessionId": "session-a", "reason": "end_turn"})
	_ = b.HandleHook(process, map[string]any{"hookEventName": "user_prompt_submit", "sessionId": "session-a"})
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
