package commands

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"hostctl/internal/client"
)

var (
	hostctlSudo = regexp.MustCompile(`(^|[;&|()\n]\s*)(/usr/local/bin/)?hostctl\s+sudo($|\s)`)
	directSudo  = regexp.MustCompile(`(^|[;&|()\n]\s*)(/usr/bin/|/bin/)?sudo($|\s)`)
)

func ContainsDirectSudo(command string) bool {
	withoutHostctl := hostctlSudo.ReplaceAllString(command, `${1}hostctl-approved${3}`)
	return directSudo.MatchString(withoutHostctl)
}

func Hook() int {
	var event map[string]any
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		return 0
	}
	socketPath := stringEnv("HOSTCTL_SOCKET", defaultRequestSocket)
	var ignored baseResponse
	_ = client.Call(socketPath, map[string]any{"op": "hook", "event": event}, &ignored)
	name, _ := event["hookEventName"].(string)
	name = strings.ToLower(strings.ReplaceAll(name, "_", ""))
	if name != "pretooluse" {
		return 0
	}
	toolName, _ := event["toolName"].(string)
	toolInput, _ := event["toolInput"].(map[string]any)
	command, _ := toolInput["command"].(string)
	if (toolName == "Bash" || toolName == "run_terminal_command") && ContainsDirectSudo(command) {
		printJSON(map[string]any{"decision": "deny", "reason": "Direct sudo is disabled. Use: hostctl sudo -- <program> <args>"})
	}
	return 0
}
