package commands

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestReadApprovalChoiceExplainsInvalidInput(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\nunknown\n C \n"))
	var output bytes.Buffer

	choice, err := readApprovalChoice(reader, &output)
	if err != nil {
		t.Fatal(err)
	}
	if choice != "c" {
		t.Fatalf("choice = %q, want c", choice)
	}
	if count := strings.Count(output.String(), approvalPrompt); count != 3 {
		t.Fatalf("prompt count = %d, want 3: %q", count, output.String())
	}
	const invalid = "Invalid choice. Enter c, m, s, d, l, or q.\n"
	if count := strings.Count(output.String(), invalid); count != 2 {
		t.Fatalf("invalid-choice count = %d, want 2: %q", count, output.String())
	}
}

func TestReadApprovalChoiceReturnsOutputError(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("c\n"))
	if _, err := readApprovalChoice(reader, failingWriter{}); err == nil || !strings.Contains(err.Error(), "write approval prompt") {
		t.Fatalf("error = %v, want approval prompt write failure", err)
	}
}
