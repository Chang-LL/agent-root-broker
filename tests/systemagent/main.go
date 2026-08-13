package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const modePrefix = "--hostctl-system-test="

func main() {
	mode := ""
	for _, argument := range os.Args[1:] {
		if strings.HasPrefix(argument, modePrefix) {
			mode = strings.TrimPrefix(argument, modePrefix)
		}
	}
	if mode == "" {
		return
	}
	session := "hostctl-system-" + strconv.Itoa(os.Getpid())
	if err := sendHook("session_start", session, ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := sendHook("user_prompt_submit", session, ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := request("/usr/bin/id", "-u")
	if mode == "message" && code == 0 {
		code = request("/usr/bin/id", "-un")
	}
	_ = sendHook("stop", session, "end_turn")
	_ = sendHook("session_end", session, "")
	os.Exit(code)
}

func sendHook(name, session, reason string) error {
	event := map[string]any{"hookEventName": name, "sessionId": session}
	if reason != "" {
		event["reason"] = reason
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode hook: %w", err)
	}
	command := exec.Command("/usr/local/libexec/hostctl-grok-hook")
	command.Stdin = bytes.NewReader(append(encoded, '\n'))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("send %s hook: %w: %s", name, err, output)
	}
	return nil
}

func request(arguments ...string) int {
	command := exec.Command("/usr/local/bin/hostctl", append([]string{"sudo", "--json", "--"}, arguments...)...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	return 0
}
