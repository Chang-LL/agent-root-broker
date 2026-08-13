package commands

import (
	"encoding/json"
	"os"
	"regexp"

	"hostctl/internal/agent"
	"hostctl/internal/client"
	"hostctl/internal/integrations/grok"
)

var (
	hostctlSudo = regexp.MustCompile(`(^|[;&|()\n]\s*)(/usr/local/bin/)?hostctl\s+sudo($|\s)`)
	directSudo  = regexp.MustCompile(`(^|[;&|()\n]\s*)(/usr/bin/|/bin/)?sudo($|\s)`)
)

func ContainsDirectSudo(command string) bool {
	withoutHostctl := hostctlSudo.ReplaceAllString(command, `${1}hostctl-approved${3}`)
	return directSudo.MatchString(withoutHostctl)
}

func GrokHook() int {
	return runAgentHook(grok.Adapter{})
}

func runAgentHook(adapter agent.HookAdapter) int {
	var raw map[string]any
	if err := json.NewDecoder(os.Stdin).Decode(&raw); err != nil {
		return 0
	}
	if event, ok, err := adapter.NormalizeLifecycle(raw); err == nil && ok {
		socketPath := stringEnv("HOSTCTL_SOCKET", defaultRequestSocket)
		var ignored baseResponse
		_ = client.Call(socketPath, map[string]any{"op": "lifecycle", "lifecycle": event}, &ignored)
	}
	if command, ok := adapter.ShellCommand(raw); ok && ContainsDirectSudo(command) {
		printJSON(map[string]any{"decision": "deny", "reason": "Direct sudo is disabled. Use: hostctl sudo -- <program> <args>"})
	}
	return 0
}
