//go:build linux

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"hostctl/internal/agent"
	"hostctl/internal/broker"
	"hostctl/internal/config"
	"hostctl/internal/executor"
	"hostctl/internal/homeaccess"
	"hostctl/internal/proc"
	"hostctl/internal/transport"
)

const maxRequestBytes = 256 * 1024

type requestEnvelope struct {
	Op             string                `json:"op"`
	Argv           []string              `json:"argv"`
	CWD            string                `json:"cwd"`
	TimeoutSeconds *int                  `json:"timeoutSeconds"`
	Lifecycle      *agent.LifecycleEvent `json:"lifecycle"`
	RequestID      string                `json:"requestId"`
	Decision       string                `json:"decision"`
	Scope          string                `json:"scope"`
	LeaseID        string                `json:"leaseId"`
	Action         string                `json:"action"`
}

type errorEnvelope struct {
	OK    bool          `json:"ok"`
	Error *broker.Error `json:"error"`
}

type okEnvelope struct {
	OK bool `json:"ok"`
}

type pendingEnvelope struct {
	OK      bool                 `json:"ok"`
	Pending []broker.PendingView `json:"pending"`
}

type leasesEnvelope struct {
	OK     bool               `json:"ok"`
	Leases []broker.LeaseView `json:"leases"`
}

type homeAccessEnvelope struct {
	OK bool `json:"ok"`
	homeaccess.Result
}

type listener struct {
	plane    string
	listener transport.Listener
	broker   *broker.Broker
	home     *homeaccess.Manager
	cfg      config.Config
}

func Run(cfg config.Config) error {
	return RunWithTransport(cfg, transport.UnixFactory{})
}

// RunWithTransport is the compile-time transport seam. The shipped request
// handlers intentionally accept only kernel-authenticated Unix peers.
func RunWithTransport(cfg config.Config, factory transport.Factory) error {
	if cfg.RequireRootDaemon && os.Geteuid() != 0 {
		return fmt.Errorf("hostctld must run as root")
	}
	if err := prepareRuntimeDir(cfg); err != nil {
		return err
	}
	requestListener, err := factory.Listen(transport.Endpoint{Plane: "request", Address: cfg.RequestSocket, AccessGroup: cfg.RequestGroup})
	if err != nil {
		return err
	}
	defer func() { _ = requestListener.Close() }()
	adminListener, err := factory.Listen(transport.Endpoint{Plane: "admin", Address: cfg.AdminSocket, AccessGroup: cfg.AdminGroup})
	if err != nil {
		return err
	}
	defer func() { _ = adminListener.Close() }()

	state := broker.New(cfg)
	requestServer := &listener{plane: "request", listener: requestListener, broker: state, cfg: cfg}
	adminServer := &listener{plane: "admin", listener: adminListener, broker: state, home: homeaccess.New(), cfg: cfg}
	errCh := make(chan error, 2)
	go func() { errCh <- requestServer.serve() }()
	go func() { errCh <- adminServer.serve() }()
	log.Printf("hostctld started")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case signalValue := <-signals:
		log.Printf("hostctld stopping on %s", signalValue)
		return nil
	case err := <-errCh:
		return err
	}
}

func prepareRuntimeDir(cfg config.Config) error {
	if err := os.MkdirAll(cfg.RuntimeDir, 0o755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if cfg.RequireRootDaemon {
		if err := os.Chown(cfg.RuntimeDir, 0, 0); err != nil {
			return fmt.Errorf("own runtime directory: %w", err)
		}
	}
	if err := os.Chmod(cfg.RuntimeDir, 0o755); err != nil {
		return fmt.Errorf("set runtime directory mode: %w", err)
	}
	return nil
}

func (s *listener) serve() error {
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, transport.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept %s socket: %w", s.plane, err)
		}
		go s.handle(connection)
	}
}

func (s *listener) handle(connection transport.Connection) {
	defer func() { _ = connection.Close() }()
	peer, err := connection.Peer()
	if err != nil {
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "unauthorized", Message: err.Error()}})
		return
	}
	if peer.Kind != transport.UnixPeerKind {
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "unauthorized", Message: "transport identity is not supported by this server"}})
		return
	}
	reader := bufio.NewReaderSize(connection, maxRequestBytes+1)
	raw, err := reader.ReadBytes('\n')
	if err != nil || len(raw) > maxRequestBytes {
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "invalid_request", Message: "request is too large or missing newline terminator"}})
		return
	}
	var request requestEnvelope
	if err := json.Unmarshal(raw, &request); err != nil {
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "invalid_json", Message: "request is not valid JSON"}})
		return
	}
	if s.plane == "request" {
		s.handleRequest(connection, reader, request, peer.PID, peer.UID)
		return
	}
	s.handleAdmin(connection, request, peer.UID)
}

func (s *listener) handleRequest(connection io.Writer, reader *bufio.Reader, request requestEnvelope, pid int, uid uint32) {
	if !authorized(uid, s.cfg.AgentUsers, s.cfg.RequestGroup) {
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "unauthorized", Message: "peer is not an authorized agent user"}})
		return
	}
	process, ok := proc.FindAgent(pid, uid, s.cfg.AgentExecutables)
	if !ok {
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "not_agent_child", Message: "request is not descended from an approved agent process"}})
		return
	}
	switch request.Op {
	case "lifecycle":
		if request.Lifecycle == nil {
			writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "invalid_lifecycle", Message: "lifecycle must be an object"}})
			return
		}
		if err := s.broker.HandleLifecycle(process, *request.Lifecycle); err != nil {
			writeJSON(connection, errorEnvelope{Error: err})
			return
		}
		writeJSON(connection, okEnvelope{OK: true})
	case "request":
		command, err := executor.Prepare(request.Argv, request.CWD, request.TimeoutSeconds, s.cfg)
		if err != nil {
			writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "invalid_command", Message: err.Error()}})
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			_, _ = io.Copy(io.Discard, reader)
			cancel()
		}()
		result, brokerErr := s.broker.Request(ctx, process, uid, command)
		if brokerErr != nil {
			writeJSON(connection, errorEnvelope{Error: brokerErr})
			return
		}
		writeJSON(connection, result)
	default:
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "invalid_operation", Message: "unknown request-plane operation"}})
	}
}

func (s *listener) handleAdmin(connection io.Writer, request requestEnvelope, uid uint32) {
	if !authorized(uid, s.cfg.ApproverUsers, s.cfg.AdminGroup) {
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "unauthorized", Message: "peer is not an authorized approver"}})
		return
	}
	switch request.Op {
	case "pending":
		writeJSON(connection, pendingEnvelope{OK: true, Pending: s.broker.Pending()})
	case "leases":
		writeJSON(connection, leasesEnvelope{OK: true, Leases: s.broker.Leases()})
	case "decide":
		if err := s.broker.Decide(request.RequestID, request.Decision, defaultScope(request.Scope), uid); err != nil {
			writeJSON(connection, errorEnvelope{Error: err})
			return
		}
		writeJSON(connection, okEnvelope{OK: true})
	case "revoke":
		if err := s.broker.Revoke(request.LeaseID, uid); err != nil {
			writeJSON(connection, errorEnvelope{Error: err})
			return
		}
		writeJSON(connection, okEnvelope{OK: true})
	case "home_access":
		home, agentUser, err := s.homeAccessTarget(uid)
		if err != nil {
			writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "home_access_unavailable", Message: err.Error()}})
			return
		}
		result, err := s.home.Manage(request.Action, home, agentUser)
		if err != nil {
			writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "home_access_failed", Message: err.Error()}})
			return
		}
		log.Printf("hostctl.audit {\"event\":\"home-access\",\"action\":%q,\"approver_uid\":%d,\"agent_user\":%q,\"state\":%q}", request.Action, uid, agentUser, result.State)
		writeJSON(connection, homeAccessEnvelope{OK: true, Result: result})
	default:
		writeJSON(connection, errorEnvelope{Error: &broker.Error{Code: "invalid_operation", Message: "unknown admin-plane operation"}})
	}
}

func (s *listener) homeAccessTarget(uid uint32) (string, string, error) {
	if uid == 0 {
		return "", "", fmt.Errorf("root cannot use approver home access")
	}
	if len(s.cfg.AgentUsers) != 1 {
		return "", "", fmt.Errorf("home access requires exactly one configured agent user")
	}
	approver, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return "", "", fmt.Errorf("look up approver: %w", err)
	}
	return homeaccess.ResolveTarget(approver.Username, s.cfg.AgentUsers[0])
}

func defaultScope(scope string) string {
	if scope == "" {
		return "command"
	}
	return scope
}

func authorized(uid uint32, allowedUsers []string, groupName string) bool {
	account, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return false
	}
	if len(allowedUsers) > 0 && !contains(allowedUsers, account.Username) {
		return false
	}
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return false
	}
	groupIDs, err := account.GroupIds()
	if err != nil {
		return false
	}
	return contains(groupIDs, group.Gid)
}

func contains[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func writeJSON(writer io.Writer, value any) {
	_ = json.NewEncoder(writer).Encode(value)
}

func Main(configPath string) int {
	cfg, err := config.Load(filepath.Clean(configPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "hostctld: %v\n", err)
		return 2
	}
	if err := Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "hostctld: %v\n", err)
		return 1
	}
	return 0
}
