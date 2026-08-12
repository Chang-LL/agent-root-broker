package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const DefaultPath = "/etc/hostctl/config.json"

type Config struct {
	RuntimeDir                 string   `json:"runtime_dir"`
	RequestSocket              string   `json:"request_socket"`
	AdminSocket                string   `json:"admin_socket"`
	RequestGroup               string   `json:"request_group"`
	AdminGroup                 string   `json:"admin_group"`
	AgentUsers                 []string `json:"agent_users"`
	ApproverUsers              []string `json:"approver_users"`
	AgentExecutables           []string `json:"agent_executables"`
	AllowedCWDRoots            []string `json:"allowed_cwd_roots"`
	CleanPath                  string   `json:"clean_path"`
	DefaultTimeoutSeconds      int      `json:"default_timeout_seconds"`
	MaxTimeoutSeconds          int      `json:"max_timeout_seconds"`
	MaxOutputBytes             int      `json:"max_output_bytes"`
	RequestTTLSeconds          int      `json:"request_ttl_seconds"`
	MessageLeaseTTLSeconds     int      `json:"message_lease_ttl_seconds"`
	SessionLeaseTTLSeconds     int      `json:"session_lease_ttl_seconds"`
	RequireRootDaemon          bool     `json:"require_root_daemon"`
	RequireRootOwnedExecutable bool     `json:"require_root_owned_executable"`
	LogArgv                    bool     `json:"log_argv"`
}

func Default() Config {
	return Config{
		RuntimeDir:                 "/run/hostctl",
		RequestSocket:              "/run/hostctl/request.sock",
		AdminSocket:                "/run/hostctl/admin.sock",
		RequestGroup:               "hostctl-agent",
		AdminGroup:                 "hostctl-approver",
		AgentUsers:                 []string{"grok-agent"},
		AgentExecutables:           []string{"/usr/local/libexec/grok-hostctl-bin"},
		AllowedCWDRoots:            []string{"/"},
		CleanPath:                  "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		DefaultTimeoutSeconds:      300,
		MaxTimeoutSeconds:          900,
		MaxOutputBytes:             1_048_576,
		RequestTTLSeconds:          300,
		MessageLeaseTTLSeconds:     900,
		SessionLeaseTTLSeconds:     14_400,
		RequireRootDaemon:          true,
		RequireRootOwnedExecutable: true,
	}
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer f.Close()
	cfg := Default()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("decode configuration: trailing JSON value")
		}
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if !filepath.IsAbs(c.RuntimeDir) {
		return fmt.Errorf("runtime_dir must be absolute")
	}
	cleanRuntime := filepath.Clean(c.RuntimeDir)
	for _, socketPath := range []string{c.RequestSocket, c.AdminSocket} {
		if filepath.Dir(socketPath) != cleanRuntime {
			return fmt.Errorf("socket paths must be directly inside runtime_dir")
		}
	}
	if len(c.AgentUsers) == 0 {
		return fmt.Errorf("agent_users must not be empty")
	}
	if len(c.AgentExecutables) == 0 {
		return fmt.Errorf("agent_executables must not be empty")
	}
	for _, path := range c.AgentExecutables {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("agent_executables must contain absolute paths")
		}
	}
	if len(c.AllowedCWDRoots) == 0 {
		return fmt.Errorf("allowed_cwd_roots must not be empty")
	}
	for _, path := range c.AllowedCWDRoots {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("allowed_cwd_roots must contain absolute paths")
		}
	}
	positive := []int{
		c.DefaultTimeoutSeconds, c.MaxTimeoutSeconds, c.MaxOutputBytes,
		c.RequestTTLSeconds, c.MessageLeaseTTLSeconds, c.SessionLeaseTTLSeconds,
	}
	for _, value := range positive {
		if value <= 0 {
			return fmt.Errorf("timeouts and output limits must be positive")
		}
	}
	if c.DefaultTimeoutSeconds > c.MaxTimeoutSeconds {
		return fmt.Errorf("default timeout cannot exceed maximum timeout")
	}
	if c.RequestGroup == "" || c.AdminGroup == "" || c.CleanPath == "" {
		return fmt.Errorf("groups and clean_path must not be empty")
	}
	return nil
}
