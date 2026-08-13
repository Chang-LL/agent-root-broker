package grok

import (
	"fmt"
	"strings"

	"hostctl/internal/agent"
)

// Adapter translates Grok Build hooks into hostctl's vendor-neutral contracts.
type Adapter struct{}

var _ agent.HookAdapter = Adapter{}

func (Adapter) NormalizeLifecycle(raw map[string]any) (agent.LifecycleEvent, bool, error) {
	name, _ := raw["hookEventName"].(string)
	name = normalizeName(name)

	var kind agent.LifecycleKind
	switch name {
	case "sessionstart":
		kind = agent.SessionStarted
	case "userpromptsubmit":
		kind = agent.TurnStarted
	case "stopfailure":
		kind = agent.TurnEnded
	case "stop":
		reason, _ := raw["reason"].(string)
		if reason != "end_turn" {
			return agent.LifecycleEvent{}, false, nil
		}
		kind = agent.TurnEnded
	case "sessionend":
		kind = agent.SessionEnded
	default:
		return agent.LifecycleEvent{}, false, nil
	}

	sessionID, _ := raw["sessionId"].(string)
	event := agent.LifecycleEvent{Kind: kind, SessionID: sessionID}
	if err := event.Validate(); err != nil {
		return agent.LifecycleEvent{}, false, fmt.Errorf("invalid Grok lifecycle hook: %w", err)
	}
	return event, true, nil
}

// ShellCommand extracts a command only from Grok tools that execute a shell.
func (Adapter) ShellCommand(raw map[string]any) (string, bool) {
	name, _ := raw["hookEventName"].(string)
	if normalizeName(name) != "pretooluse" {
		return "", false
	}
	toolName, _ := raw["toolName"].(string)
	if toolName != "Bash" && toolName != "run_terminal_command" {
		return "", false
	}
	toolInput, _ := raw["toolInput"].(map[string]any)
	command, ok := toolInput["command"].(string)
	return command, ok
}

func normalizeName(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "_", ""))
}
