package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const modePrefix = "--hostctl-system-test="
const gatePrefix = "--hostctl-test-gate="

func main() {
	mode := ""
	gate := ""
	for _, argument := range os.Args[1:] {
		if strings.HasPrefix(argument, modePrefix) {
			mode = strings.TrimPrefix(argument, modePrefix)
		}
		if strings.HasPrefix(argument, gatePrefix) {
			gate = strings.TrimPrefix(argument, gatePrefix)
		}
	}
	if mode == "" {
		return
	}
	if mode == "missing" {
		os.Exit(request("/usr/bin/id", "-u"))
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

	code := 0
	if mode == "timeout" {
		code = requestWithTimeout(1, "/bin/sleep", "2")
	} else {
		code = request("/usr/bin/id", "-u")
	}
	switch mode {
	case "message", "command":
		if code == 0 {
			code = request("/usr/bin/id", "-un")
		}
	case "session":
		if code == 0 {
			_ = sendHook("stop", session, "end_turn")
			_ = sendHook("user_prompt_submit", session, "")
			code = request("/usr/bin/id", "-un")
		}
	case "revoke":
		if code == 0 {
			if err := waitGate(gate); err != nil {
				fmt.Fprintln(os.Stderr, err)
				code = 1
			} else {
				code = request("/usr/bin/id", "-un")
			}
		}
	case "restart":
		if code == 0 {
			if err := waitGate(gate); err != nil {
				fmt.Fprintln(os.Stderr, err)
				code = 1
			} else {
				_ = request("/usr/bin/true")
				_ = sendHook("session_start", session, "")
				_ = sendHook("user_prompt_submit", session, "")
				code = request("/usr/bin/id", "-un")
			}
		}
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
	return runRequest(append([]string{"sudo", "--json", "--"}, arguments...)...)
}

func requestWithTimeout(timeout int, arguments ...string) int {
	prefix := []string{"sudo", "--json", "--timeout", strconv.Itoa(timeout), "--"}
	return runRequest(append(prefix, arguments...)...)
}

func runRequest(arguments ...string) int {
	command := exec.Command("/usr/local/bin/hostctl", arguments...)
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

func waitGate(path string) error {
	if path == "" {
		return fmt.Errorf("missing gate path")
	}
	if err := os.WriteFile(path+".ready", []byte("ready\n"), 0o600); err != nil {
		return err
	}
	for attempts := 0; attempts < 200; attempts++ {
		if _, err := os.Stat(path + ".continue"); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("gate timed out")
}
