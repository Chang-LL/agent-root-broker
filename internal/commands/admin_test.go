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
	renderer := newAdminRenderer(defaultAdminUIConfig(), false, 100)

	choice, err := readApprovalChoice(reader, &output, renderer, 7)
	if err != nil {
		t.Fatal(err)
	}
	if choice != "c" {
		t.Fatalf("choice = %q, want c", choice)
	}
	if count := strings.Count(output.String(), "Approve\n"); count != 1 {
		t.Fatalf("full prompt count = %d, want 1: %q", count, output.String())
	}
	if count := strings.Count(output.String(), "Choice: "); count != 3 {
		t.Fatalf("choice prompt count = %d, want 3: %q", count, output.String())
	}
	const invalid = "Invalid choice. Enter c, m, s, d, l, or q.\n"
	if count := strings.Count(output.String(), invalid); count != 2 {
		t.Fatalf("invalid-choice count = %d, want 2: %q", count, output.String())
	}
}

func TestReadApprovalChoiceReturnsOutputError(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("c\n"))
	renderer := newAdminRenderer(defaultAdminUIConfig(), false, 100)
	if _, err := readApprovalChoice(reader, failingWriter{}, renderer, 1); err == nil || !strings.Contains(err.Error(), "write approval prompt") {
		t.Fatalf("error = %v, want approval prompt write failure", err)
	}
}
