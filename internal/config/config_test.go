package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownAndTrailingData(t *testing.T) {
	tests := []struct{ name, body, wanted string }{
		{"unknown", `{"surprise":true}`, "unknown field"},
		{"trailing", `{}` + `{}`, "trailing JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("got %v, want %q", err, test.wanted)
			}
		})
	}
}

func TestValidateRejectsSocketOutsideRuntime(t *testing.T) {
	cfg := Default()
	cfg.RequestSocket = "/tmp/request.sock"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
