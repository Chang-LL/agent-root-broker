package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"

	"hostctl/internal/client"
)

type commandResponse struct {
	baseResponse
	RequestID       json.RawMessage `json:"requestId,omitempty"`
	ApprovalScope   string          `json:"approvalScope,omitempty"`
	CommandHash     string          `json:"commandHash,omitempty"`
	ExitCode        *int            `json:"exitCode,omitempty"`
	Stdout          *string         `json:"stdout,omitempty"`
	Stderr          *string         `json:"stderr,omitempty"`
	TimedOut        *bool           `json:"timedOut,omitempty"`
	DurationMS      *int64          `json:"durationMs,omitempty"`
	StdoutTruncated *bool           `json:"stdoutTruncated,omitempty"`
	StderrTruncated *bool           `json:"stderrTruncated,omitempty"`
}

func Hostctl(args []string, version string) int {
	socketPath := stringEnv("HOSTCTL_SOCKET", defaultRequestSocket)
	for len(args) >= 2 && args[0] == "--socket" {
		socketPath, args = args[1], args[2:]
	}
	globalJSON := false
	if len(args) > 0 && args[0] == "--json" {
		globalJSON, args = true, args[1:]
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stdout, "Usage: hostctl [--socket PATH] sudo [--json] [--timeout SECONDS] -- PROGRAM [ARGS...]\n       hostctl [--json] doctor\n       hostctl version")
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintf(os.Stdout, "hostctl %s\n", version)
		return 0
	}
	if args[0] == "doctor" {
		if len(args) == 2 && args[1] == "--json" {
			globalJSON = true
		} else if len(args) != 1 {
			printClientError("hostctl", globalJSON, fmt.Errorf("doctor takes no arguments"))
			return 2
		}
		return doctor(socketPath, version, globalJSON)
	}
	if args[0] != "sudo" {
		fmt.Fprintf(os.Stderr, "hostctl: unknown command %q\n", args[0])
		return 2
	}
	args = args[1:]
	asJSON := globalJSON
	var timeout *int
	for len(args) > 0 {
		switch args[0] {
		case "--":
			args = args[1:]
			goto parsed
		case "--json":
			asJSON, args = true, args[1:]
		case "--timeout":
			if len(args) < 2 {
				printClientError("hostctl", asJSON, fmt.Errorf("--timeout requires a value"))
				return 2
			}
			value, err := strconv.Atoi(args[1])
			if err != nil {
				printClientError("hostctl", asJSON, fmt.Errorf("invalid timeout: %s", args[1]))
				return 2
			}
			timeout, args = &value, args[2:]
		default:
			goto parsed
		}
	}

parsed:
	if len(args) == 0 {
		printClientError("hostctl", asJSON, fmt.Errorf("missing command after 'hostctl sudo --'"))
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		printClientError("hostctl", asJSON, err)
		return 125
	}
	payload := struct {
		Op             string   `json:"op"`
		Argv           []string `json:"argv"`
		CWD            string   `json:"cwd"`
		TimeoutSeconds *int     `json:"timeoutSeconds,omitempty"`
	}{"request", args, cwd, timeout}
	var response commandResponse
	if err := client.Call(socketPath, payload, &response); err != nil {
		printClientError("hostctl", asJSON, err)
		return 125
	}
	if asJSON {
		printJSON(response)
	} else if response.OK {
		if response.Stdout != nil {
			fmt.Fprint(os.Stdout, *response.Stdout)
		}
		if response.Stderr != nil {
			fmt.Fprint(os.Stderr, *response.Stderr)
		}
	} else if response.Error != nil {
		fmt.Fprintf(os.Stderr, "hostctl: %s\n", response.Error.Message)
	}
	if response.OK {
		if response.ExitCode != nil {
			return *response.ExitCode
		}
		return 1
	}
	if response.Error != nil && response.Error.Code == "expired" {
		return 124
	}
	return 126
}

type doctorResponse struct {
	OK            bool   `json:"ok"`
	Version       string `json:"version"`
	Platform      string `json:"platform"`
	RequestSocket string `json:"requestSocket"`
	SocketPresent bool   `json:"socketPresent"`
	SocketIsUnix  bool   `json:"socketIsUnix"`
	MissingSetup  string `json:"missingSetup,omitempty"`
}

func doctor(socketPath, version string, asJSON bool) int {
	response := doctorResponse{Version: version, Platform: runtime.GOOS + "/" + runtime.GOARCH, RequestSocket: socketPath}
	info, err := os.Lstat(socketPath)
	if err == nil {
		response.SocketPresent = true
		response.SocketIsUnix = info.Mode()&os.ModeSocket != 0
	}
	response.OK = response.SocketPresent && response.SocketIsUnix
	if !response.OK {
		response.MissingSetup = "install and start hostctld"
	}
	if asJSON {
		printJSON(response)
	} else {
		fmt.Fprintf(os.Stdout, "hostctl %s (%s)\nrequest socket: %s\npresent: %t  unix socket: %t\n", response.Version, response.Platform, response.RequestSocket, response.SocketPresent, response.SocketIsUnix)
		if response.MissingSetup != "" {
			fmt.Fprintf(os.Stdout, "missing setup: %s\n", response.MissingSetup)
		}
	}
	if response.OK {
		return 0
	}
	return 1
}
