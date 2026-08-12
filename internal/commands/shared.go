package commands

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	defaultRequestSocket = "/run/hostctl/request.sock"
	defaultAdminSocket   = "/run/hostctl/admin.sock"
)

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type baseResponse struct {
	OK    bool           `json:"ok"`
	Error *responseError `json:"error,omitempty"`
}

func printJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func printClientError(program string, asJSON bool, err error) {
	if asJSON {
		printJSON(baseResponse{Error: &responseError{Code: "client_error", Message: err.Error()}})
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", program, err)
}

func stringEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
