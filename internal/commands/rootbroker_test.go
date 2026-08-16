package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommandResponsePreservesNullRequestAndZeroExitCode(t *testing.T) {
	input := `{"ok":true,"requestId":null,"approvalScope":"message","commandHash":"abc","exitCode":0,"stdout":"","stderr":"","timedOut":false,"durationMs":0,"stdoutTruncated":false,"stderrTruncated":false}`
	var response commandResponse
	if err := json.Unmarshal([]byte(input), &response); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, wanted := range []string{`"requestId":null`, `"exitCode":0`, `"stdout":""`, `"timedOut":false`} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("response omitted %s: %s", wanted, text)
		}
	}
}

func TestDoctorJSONShape(t *testing.T) {
	path := t.TempDir() + "/missing.sock"
	if code := doctor(path, "test", true); code != 1 {
		t.Fatalf("doctor returned %d", code)
	}
}
