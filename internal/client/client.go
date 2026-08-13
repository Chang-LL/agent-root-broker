package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const maxResponseBytes = 4 * 1024 * 1024

func Call(socketPath string, payload any, response any) error {
	connection, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("cannot reach hostctl broker at %s: %w", socketPath, err)
	}
	defer func() { _ = connection.Close() }()
	if err := json.NewEncoder(connection).Encode(payload); err != nil {
		return fmt.Errorf("send broker request: %w", err)
	}
	reader := bufio.NewReaderSize(connection, 64*1024)
	limited := &limitedReader{reader: reader, remaining: maxResponseBytes}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(response); err != nil {
		return fmt.Errorf("broker returned invalid JSON: %w", err)
	}
	return nil
}

type limitedReader struct {
	reader    *bufio.Reader
	remaining int
}

func (r *limitedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, fmt.Errorf("broker response exceeded size limit")
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= n
	return n, err
}
