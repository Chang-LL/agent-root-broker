package agent

import "fmt"

// LifecycleKind is a vendor-neutral state transition understood by the broker.
type LifecycleKind string

const (
	SessionStarted LifecycleKind = "session_started"
	TurnStarted    LifecycleKind = "turn_started"
	TurnEnded      LifecycleKind = "turn_ended"
	SessionEnded   LifecycleKind = "session_ended"
)

// LifecycleEvent is the normalized contract between an agent integration and
// the rootbroker broker. Vendor hook payloads must be adapted before this point.
type LifecycleEvent struct {
	Kind      LifecycleKind `json:"kind"`
	SessionID string        `json:"sessionId"`
}

func (e LifecycleEvent) Validate() error {
	if e.SessionID == "" || len(e.SessionID) > 256 {
		return fmt.Errorf("sessionId must contain between 1 and 256 bytes")
	}
	switch e.Kind {
	case SessionStarted, TurnStarted, TurnEnded, SessionEnded:
		return nil
	default:
		return fmt.Errorf("unknown lifecycle kind %q", e.Kind)
	}
}

// LifecycleAdapter converts one agent vendor's hook payload into rootbroker's
// normalized lifecycle contract. The boolean is false for unrelated hooks.
type LifecycleAdapter interface {
	NormalizeLifecycle(map[string]any) (LifecycleEvent, bool, error)
}

// HookAdapter also extracts shell commands for integration-side guardrails.
// It lets the generic hook bridge remain independent of vendor payload fields.
type HookAdapter interface {
	LifecycleAdapter
	ShellCommand(map[string]any) (string, bool)
}
