package homeaccess

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
)

type Result struct {
	State     string `json:"state"`
	Home      string `json:"home"`
	AgentUser string `json:"agentUser"`
}

type operationFunc func(string, string, uint32) (string, error)
type lookupUIDFunc func(string) (uint32, error)

type Manager struct {
	mu        sync.Mutex
	operation operationFunc
	lookupUID lookupUIDFunc
}

func New() *Manager {
	return &Manager{operation: platformManage, lookupUID: lookupUID}
}

func (m *Manager) Manage(action, home, agentUser string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	home = filepath.Clean(home)
	if !filepath.IsAbs(home) || home == "/" {
		return Result{}, fmt.Errorf("refusing unsafe home directory")
	}
	if !validUserName(agentUser) {
		return Result{}, fmt.Errorf("invalid agent user name")
	}
	if action != "status" && action != "grant" && action != "revoke" {
		return Result{}, fmt.Errorf("unknown home-access action: %s", action)
	}
	uid, err := m.lookupUID(agentUser)
	if err != nil {
		return Result{}, fmt.Errorf("look up non-root agent user: %w", err)
	}
	if uid == 0 {
		return Result{}, fmt.Errorf("agent user must not be root")
	}
	state, err := m.operation(action, home, uid)
	if err != nil {
		return Result{}, err
	}
	return Result{State: state, Home: home, AgentUser: agentUser}, nil
}

func lookupUID(name string) (uint32, error) {
	account, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(account.Uid, 10, 32)
	return uint32(value), err
}

func validUserName(value string) bool {
	if value == "" {
		return false
	}
	for i, character := range value {
		if (character >= 'a' && character <= 'z') || character == '_' || (i > 0 && character >= '0' && character <= '9') || (i > 0 && character == '-') || (i == len(value)-1 && character == '$') {
			continue
		}
		return false
	}
	return true
}
