package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Chang-LL/rootbroker/internal/config"
)

var blockedExecutables = map[string]bool{
	"rootbroker": true, "rootbroker-admin": true, "rootbroker-maint": true, "rootbrokerd": true,
	"rootbroker-grok-hook": true, "pkexec": true, "su": true, "sudo": true,
	"rootbroker-bin": true,
}

var interpreters = map[string]bool{
	"bash": true, "dash": true, "fish": true, "node": true, "perl": true,
	"python": true, "python3": true, "ruby": true, "sh": true, "zsh": true,
}

type Command struct {
	Argv           []string `json:"argv"`
	CWD            string   `json:"cwd"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	Hash           string   `json:"hash"`
	Risks          []string `json:"risks"`
}

type Result struct {
	ExitCode        int    `json:"exitCode"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	TimedOut        bool   `json:"timedOut"`
	DurationMS      int64  `json:"durationMs"`
	StdoutTruncated bool   `json:"stdoutTruncated"`
	StderrTruncated bool   `json:"stderrTruncated"`
}

func resolveExecutable(name, cleanPath, cwd string) (string, error) {
	var candidate string
	if strings.ContainsRune(name, filepath.Separator) {
		candidate = name
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(cwd, candidate)
		}
	} else {
		for _, directory := range filepath.SplitList(cleanPath) {
			path := filepath.Join(directory, name)
			info, err := os.Stat(path)
			if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
				candidate = path
				break
			}
		}
	}
	if candidate == "" {
		return "", fmt.Errorf("executable not found in broker PATH: %s", name)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func within(path string, roots []string) bool {
	for _, root := range roots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(resolvedRoot, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func Prepare(argv []string, cwd string, timeoutSeconds *int, cfg config.Config) (Command, error) {
	if len(argv) == 0 {
		return Command{}, fmt.Errorf("command must be a non-empty argv array")
	}
	if len(argv) > 256 {
		return Command{}, fmt.Errorf("command has too many arguments")
	}
	total := 0
	for _, arg := range argv {
		if strings.ContainsRune(arg, 0) {
			return Command{}, fmt.Errorf("command arguments must not contain NUL bytes")
		}
		total += len(arg)
	}
	if total > 65_536 {
		return Command{}, fmt.Errorf("command is too large")
	}

	realCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return Command{}, fmt.Errorf("resolve working directory: %w", err)
	}
	info, err := os.Stat(realCWD)
	if err != nil || !info.IsDir() {
		return Command{}, fmt.Errorf("working directory does not exist: %s", realCWD)
	}
	if !within(realCWD, cfg.AllowedCWDRoots) {
		return Command{}, fmt.Errorf("working directory is outside allowed_cwd_roots")
	}

	executable, err := resolveExecutable(argv[0], cfg.CleanPath, realCWD)
	if err != nil {
		return Command{}, err
	}
	base := filepath.Base(executable)
	if blockedExecutables[base] {
		return Command{}, fmt.Errorf("recursive or privilege-wrapper executable is not allowed: %s", base)
	}
	if err := validateExecutable(executable, cfg.RequireRootOwnedExecutable); err != nil {
		return Command{}, err
	}

	timeout := cfg.DefaultTimeoutSeconds
	if timeoutSeconds != nil {
		timeout = *timeoutSeconds
	}
	if timeout <= 0 || timeout > cfg.MaxTimeoutSeconds {
		return Command{}, fmt.Errorf("timeout must be between 1 and %d seconds", cfg.MaxTimeoutSeconds)
	}

	resolvedArgv := append([]string{executable}, argv[1:]...)
	risks := make([]string, 0, 3)
	if interpreters[base] {
		risks = append(risks, "interpreter-or-shell")
	}
	for _, arg := range argv[1:] {
		if arg == "-c" || arg == "--command" || arg == "-m" {
			risks = append(risks, "executes-inline-or-module-code")
			break
		}
	}
	for _, arg := range argv[1:] {
		if strings.ContainsAny(arg, "*?") {
			risks = append(risks, "wildcard-is-literal-without-shell")
			break
		}
	}
	canonical, _ := json.Marshal(struct {
		Argv           []string `json:"argv"`
		CWD            string   `json:"cwd"`
		TimeoutSeconds int      `json:"timeoutSeconds"`
	}{resolvedArgv, realCWD, timeout})
	digest := sha256.Sum256(canonical)
	return Command{
		Argv: resolvedArgv, CWD: realCWD, TimeoutSeconds: timeout,
		Hash: hex.EncodeToString(digest[:]), Risks: risks,
	}, nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = w.buffer.Write(p[:remaining])
		} else {
			_, _ = w.buffer.Write(p)
		}
	}
	if original > remaining {
		w.truncated = true
	}
	return original, nil
}

func Execute(command Command, cfg config.Config) Result {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(command.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.Command(command.Argv[0], command.Argv[1:]...)
	cmd.Dir = command.CWD
	cmd.Env = []string{
		"HOME=/root", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "LOGNAME=root",
		"PATH=" + cfg.CleanPath, "SHELL=/bin/sh", "USER=root",
	}
	cmd.Stdin = nil
	cmd.SysProcAttr = processGroupAttributes()
	stdout := &limitedBuffer{limit: cfg.MaxOutputBytes}
	stderr := &limitedBuffer{limit: cfg.MaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: 126, Stderr: "rootbroker: execution failed: " + err.Error() + "\n", DurationMS: time.Since(started).Milliseconds()}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
	timedOut := false
	select {
	case runErr = <-done:
	case <-ctx.Done():
		timedOut = true
		killProcessGroup(cmd.Process.Pid)
		runErr = <-done
	}
	exitCode := 0
	if runErr != nil {
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 126
		}
	}
	if timedOut {
		exitCode = 124
		_, _ = stderr.Write([]byte(fmt.Sprintf("rootbroker: command timed out after %ds\n", command.TimeoutSeconds)))
	}
	return Result{
		ExitCode: exitCode, Stdout: stdout.buffer.String(), Stderr: stderr.buffer.String(),
		TimedOut: timedOut, DurationMS: time.Since(started).Milliseconds(),
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
}
