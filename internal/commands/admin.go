package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Chang-LL/agent-root-broker/internal/broker"
	"github.com/Chang-LL/agent-root-broker/internal/client"
)

type pendingResponse struct {
	baseResponse
	Pending []broker.PendingView `json:"pending"`
}

type leasesResponse struct {
	baseResponse
	Leases []broker.LeaseView `json:"leases"`
}

type homeAccessResponse struct {
	baseResponse
	State     string `json:"state"`
	Home      string `json:"home"`
	AgentUser string `json:"agentUser"`
}

func Admin(args []string) int {
	socketPath := stringEnv("ROOTBROKER_ADMIN_SOCKET", defaultAdminSocket)
	asJSON := false
	for len(args) > 0 {
		if len(args) >= 2 && args[0] == "--socket" {
			socketPath, args = args[1], args[2:]
			continue
		}
		if args[0] == "--json" {
			asJSON, args = true, args[1:]
			continue
		}
		break
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stdout, "Usage: rootbroker-admin [--json] pending|leases\n       rootbroker-admin approve REQUEST_ID [--scope command|message|session]\n       rootbroker-admin deny REQUEST_ID\n       rootbroker-admin revoke LEASE_ID\n       rootbroker-admin home-access status|grant|revoke\n       rootbroker-admin watch [OPTIONS]")
		return 0
	}
	switch args[0] {
	case "pending":
		response, err := getPending(socketPath)
		if err != nil {
			printClientError("rootbroker-admin", asJSON, err)
			return 1
		}
		if asJSON {
			printJSON(response)
		} else if len(response.Pending) == 0 {
			fmt.Fprintln(os.Stdout, "No pending requests.")
		} else {
			for _, item := range response.Pending {
				fmt.Fprintln(os.Stdout, formatPending(item))
			}
		}
		return responseExit(response.baseResponse, asJSON)
	case "leases":
		var response leasesResponse
		err := client.Call(socketPath, map[string]any{"op": "leases"}, &response)
		if err != nil {
			printClientError("rootbroker-admin", asJSON, err)
			return 1
		}
		if asJSON {
			printJSON(response)
		} else if len(response.Leases) == 0 {
			fmt.Fprintln(os.Stdout, "No active leases.")
		} else {
			for _, item := range response.Leases {
				turn := "-"
				if item.Turn != nil {
					turn = strconv.Itoa(*item.Turn)
				}
				fmt.Fprintf(os.Stdout, "%s  scope=%s  provider=%s  session=%s  turn=%s  expires=%s\n", item.ID, item.Scope, item.DecisionProvider, item.SessionID, turn, time.Unix(int64(item.ExpiresAt), 0).Format("2006-01-02 15:04:05"))
			}
		}
		return responseExit(response.baseResponse, asJSON)
	case "approve":
		if len(args) < 2 {
			return adminUsageError(asJSON, "approve requires REQUEST_ID")
		}
		scope := "command"
		if len(args) == 4 && args[2] == "--scope" {
			scope = args[3]
		} else if len(args) != 2 {
			return adminUsageError(asJSON, "invalid approve arguments")
		}
		return adminMutation(socketPath, asJSON, map[string]any{"op": "decide", "requestId": args[1], "decision": "approved", "scope": scope})
	case "deny":
		if len(args) != 2 {
			return adminUsageError(asJSON, "deny requires REQUEST_ID")
		}
		return adminMutation(socketPath, asJSON, map[string]any{"op": "decide", "requestId": args[1], "decision": "denied", "scope": "command"})
	case "revoke":
		if len(args) != 2 {
			return adminUsageError(asJSON, "revoke requires LEASE_ID")
		}
		return adminMutation(socketPath, asJSON, map[string]any{"op": "revoke", "leaseId": args[1]})
	case "home-access":
		if len(args) != 2 || (args[1] != "status" && args[1] != "grant" && args[1] != "revoke") {
			return adminUsageError(asJSON, "home-access requires status, grant, or revoke")
		}
		if args[1] == "grant" && !asJSON {
			fmt.Fprintln(os.Stderr, "Warning: granting full-home access includes SSH keys, application credentials, startup files, and personal files.")
			fmt.Fprintln(os.Stderr, "The agent may be able to impersonate the approver; human-only approval will no longer be a strong isolation boundary.")
		}
		var response homeAccessResponse
		if err := client.Call(socketPath, map[string]any{"op": "home_access", "action": args[1]}, &response); err != nil {
			printClientError("rootbroker-admin", asJSON, err)
			return 1
		}
		if asJSON {
			printJSON(response)
		} else if !response.OK && response.Error != nil {
			fmt.Fprintf(os.Stderr, "rootbroker-admin: %s\n", response.Error.Message)
		} else {
			switch args[1] {
			case "grant":
				fmt.Fprintf(os.Stdout, "Full-home access enabled: %s can read and write %s.\n", response.AgentUser, response.Home)
			case "revoke":
				fmt.Fprintf(os.Stdout, "Full-home access disabled: removed %s ACLs from %s.\n", response.AgentUser, response.Home)
			default:
				fmt.Fprintf(os.Stdout, "Full-home access: %s  agent=%s  home=%s\n", response.State, response.AgentUser, response.Home)
			}
		}
		return responseExit(response.baseResponse, asJSON)
	case "watch":
		options, help, err := parseAdminWatchOptions(args[1:])
		if help {
			fmt.Fprintln(os.Stdout, adminWatchUsage())
			return 0
		}
		if err != nil {
			return adminUsageError(asJSON, err.Error())
		}
		isTTY, width := adminTerminal(os.Stdout)
		renderer := newAdminRenderer(options.UI, shouldUseANSI(options.UI.Color, isTTY, os.Getenv("TERM")), width)
		return watch(socketPath, options.Interval, renderer)
	default:
		return adminUsageError(asJSON, "unknown command: "+args[0])
	}
}

func getPending(socketPath string) (pendingResponse, error) {
	var response pendingResponse
	err := client.Call(socketPath, map[string]any{"op": "pending"}, &response)
	if err == nil && !response.OK && response.Error != nil {
		err = fmt.Errorf("%s", response.Error.Message)
	}
	return response, err
}

func adminMutation(socketPath string, asJSON bool, payload map[string]any) int {
	var response baseResponse
	if err := client.Call(socketPath, payload, &response); err != nil {
		printClientError("rootbroker-admin", asJSON, err)
		return 1
	}
	if asJSON {
		printJSON(response)
	}
	if !response.OK && response.Error != nil && !asJSON {
		fmt.Fprintf(os.Stderr, "rootbroker-admin: %s\n", response.Error.Message)
	}
	return responseExit(response, asJSON)
}

func responseExit(response baseResponse, _ bool) int {
	if response.OK {
		return 0
	}
	return 1
}

func adminUsageError(asJSON bool, message string) int {
	printClientError("rootbroker-admin", asJSON, fmt.Errorf("%s", message))
	return 2
}

func formatPending(item broker.PendingView) string {
	quoted := make([]string, len(item.Command.Argv))
	for i, arg := range item.Command.Argv {
		quoted[i] = shellQuote(arg)
	}
	risks := "none detected"
	if len(item.Command.Risks) > 0 {
		risks = strings.Join(item.Command.Risks, ", ")
	}
	return fmt.Sprintf("request: %s\nsession: %s  turn: %d\ncommand: %s\ncwd: %s\ntimeout: %ds\nhash: %s\nrisks: %s\n", item.ID, item.SessionID, item.Turn, strings.Join(quoted, " "), item.Command.CWD, item.Command.TimeoutSeconds, item.Command.Hash, risks)
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|()<>*?![]{}") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func readApprovalChoice(reader *bufio.Reader, output io.Writer, renderer adminRenderer, turn int) (string, error) {
	fullPrompt := true
	for {
		if _, err := fmt.Fprint(output, renderer.approvalPrompt(turn, fullPrompt)); err != nil {
			return "", fmt.Errorf("write approval prompt: %w", err)
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		choice := strings.ToLower(strings.TrimSpace(line))
		switch choice {
		case "c", "m", "s", "d", "l", "q":
			return choice, nil
		default:
			if _, err := fmt.Fprintln(output, renderer.invalidChoice()); err != nil {
				return "", fmt.Errorf("write invalid-choice message: %w", err)
			}
			fullPrompt = false
		}
	}
}

func watch(socketPath string, interval time.Duration, renderer adminRenderer) int {
	fmt.Fprintln(os.Stdout, "Waiting for rootbroker requests. Ctrl+C quits.")
	reader := bufio.NewReader(os.Stdin)
	seen := make(map[string]bool)
	for {
		response, err := getPending(socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rootbroker-admin: %v\n", err)
			return 1
		}
		var current *broker.PendingView
		for i := range response.Pending {
			if !seen[response.Pending[i].ID] {
				current = &response.Pending[i]
				break
			}
		}
		if current == nil {
			time.Sleep(interval)
			continue
		}
		fmt.Fprintln(os.Stdout, "\n"+renderer.pending(*current)+"\n")
		for {
			choice, err := readApprovalChoice(reader, os.Stdout, renderer, current.Turn)
			if err != nil {
				return 1
			}
			scope := map[string]string{"c": "command", "m": "message", "s": "session"}[choice]
			if scope != "" {
				if code := adminMutation(socketPath, false, map[string]any{"op": "decide", "requestId": current.ID, "decision": "approved", "scope": scope}); code != 0 {
					return code
				}
				fmt.Fprintln(os.Stdout, renderer.approved(current.ID, scope))
				break
			}
			if choice == "d" {
				if code := adminMutation(socketPath, false, map[string]any{"op": "decide", "requestId": current.ID, "decision": "denied", "scope": "command"}); code != 0 {
					return code
				}
				fmt.Fprintln(os.Stdout, renderer.denied(current.ID))
				break
			}
			if choice == "l" {
				seen[current.ID] = true
				break
			}
			if choice == "q" {
				return 0
			}
		}
	}
}
