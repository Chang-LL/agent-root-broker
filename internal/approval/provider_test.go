package approval

import (
	"context"
	"testing"
	"time"

	"github.com/Chang-LL/rootbroker/internal/proc"
)

func TestManualProviderMessageReviewResolvesSameTurn(t *testing.T) {
	provider := NewManualProvider()
	process := proc.Identity{PID: 1, UID: 2, StartTime: 3}
	decisions := make(chan Decision, 2)
	for _, id := range []string{"a", "b"} {
		request := Request{
			ID: id, Process: process, SessionID: "session", Turn: 4,
			CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
		}
		go func() {
			decision, _ := provider.Decide(context.Background(), request)
			decisions <- decision
		}()
	}
	waitForPending(t, provider, 2)
	if _, err := provider.Review("a", Approved, MessageScope, 1000); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		decision := <-decisions
		if decision.Outcome != Approved || decision.Scope != MessageScope || decision.ApproverUID != 1000 ||
			decision.Provider != ManualProviderName || decision.Principal != "uid:1000" {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	}
}

func TestManualProviderObservesCancellation(t *testing.T) {
	provider := NewManualProvider()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan Decision, 1)
	go func() {
		decision, _ := provider.Decide(ctx, Request{ID: "cancel", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)})
		result <- decision
	}()
	waitForPending(t, provider, 1)
	cancel()
	if decision := <-result; decision.Outcome != Cancelled {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestDecisionValidation(t *testing.T) {
	for _, decision := range []Decision{
		{Outcome: Approved, Scope: CommandScope},
		{Outcome: Approved, Scope: "invalid", Provider: "test", Principal: "rule"},
		{Outcome: Denied, Scope: MessageScope, Provider: "test", Principal: "rule"},
		{Outcome: "invalid"},
	} {
		if err := decision.Validate(); err == nil {
			t.Fatalf("invalid decision passed validation: %+v", decision)
		}
	}
	if err := (Decision{
		Outcome: Approved, Scope: CommandScope, Provider: "test", Principal: "rule",
	}).Validate(); err != nil {
		t.Fatalf("valid decision was rejected: %v", err)
	}
	if err := (Decision{Outcome: Cancelled}).Validate(); err != nil {
		t.Fatalf("valid cancellation was rejected: %v", err)
	}
}

func waitForPending(t *testing.T, provider *ManualProvider, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(provider.Pending()) == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("provider did not expose %d pending requests", count)
}
