package commands

import (
	"encoding/json"
	"os"
	"regexp"

	"github.com/Chang-LL/agent-root-broker/internal/agent"
	"github.com/Chang-LL/agent-root-broker/internal/client"
	"github.com/Chang-LL/agent-root-broker/internal/integrations/grok"
)

var (
	rootbrokerSudo = regexp.MustCompile(`(^|[;&|()\n]\s*)(/usr/local/bin/)?rootbroker\s+sudo($|\s)`)
	directSudo     = regexp.MustCompile(`(^|[;&|()\n]\s*)(/usr/bin/|/bin/)?sudo($|\s)`)
)

func ContainsDirectSudo(command string) bool {
	withoutRootbroker := rootbrokerSudo.ReplaceAllString(command, `${1}rootbroker-approved${3}`)
	return directSudo.MatchString(withoutRootbroker)
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
		socketPath := stringEnv("ROOTBROKER_SOCKET", defaultRequestSocket)
		var ignored baseResponse
		_ = client.Call(socketPath, map[string]any{"op": "lifecycle", "lifecycle": event}, &ignored)
	}
	if command, ok := adapter.ShellCommand(raw); ok && ContainsDirectSudo(command) {
		printJSON(map[string]any{"decision": "deny", "reason": "Direct sudo is disabled. Use: rootbroker sudo -- <program> <args>"})
	}
	return 0
}
