package grok

import (
	"strings"
	"testing"

	"hostctl/internal/agent"
)

func TestNormalizeLifecycle(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
		kind agent.LifecycleKind
		ok   bool
	}{
		{"session start", map[string]any{"hookEventName": "session_start", "sessionId": "s"}, agent.SessionStarted, true},
		{"turn start", map[string]any{"hookEventName": "user_prompt_submit", "sessionId": "s"}, agent.TurnStarted, true},
		{"turn end", map[string]any{"hookEventName": "stop", "sessionId": "s", "reason": "end_turn"}, agent.TurnEnded, true},
		{"failed turn", map[string]any{"hookEventName": "stop_failure", "sessionId": "s"}, agent.TurnEnded, true},
		{"session end", map[string]any{"hookEventName": "session_end", "sessionId": "s"}, agent.SessionEnded, true},
		{"unrelated stop", map[string]any{"hookEventName": "stop", "sessionId": "s", "reason": "tool"}, "", false},
		{"tool hook", map[string]any{"hookEventName": "pre_tool_use", "sessionId": "s"}, "", false},
	}
	adapter := Adapter{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := adapter.NormalizeLifecycle(tt.raw)
			if err != nil || ok != tt.ok || got.Kind != tt.kind {
				t.Fatalf("NormalizeLifecycle() = %+v, %t, %v", got, ok, err)
			}
			if ok && got.SessionID != "s" {
				t.Fatalf("unexpected session ID %q", got.SessionID)
			}
		})
	}
}

func TestNormalizeLifecycleRejectsInvalidSession(t *testing.T) {
	_, ok, err := (Adapter{}).NormalizeLifecycle(map[string]any{
		"hookEventName": "session_start",
		"sessionId":     strings.Repeat("x", 257),
	})
	if err == nil || ok {
		t.Fatalf("expected invalid session error, got ok=%t err=%v", ok, err)
	}
}

func TestShellCommand(t *testing.T) {
	adapter := Adapter{}
	command, ok := adapter.ShellCommand(map[string]any{
		"hookEventName": "pre_tool_use",
		"toolName":      "Bash",
		"toolInput":     map[string]any{"command": "sudo id"},
	})
	if !ok || command != "sudo id" {
		t.Fatalf("ShellCommand() = %q, %t", command, ok)
	}
	if _, ok := adapter.ShellCommand(map[string]any{
		"hookEventName": "pre_tool_use",
		"toolName":      "Read",
		"toolInput":     map[string]any{"command": "sudo id"},
	}); ok {
		t.Fatal("non-shell tool was accepted")
	}
}
