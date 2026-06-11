package monitor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const clientTimeout = 2 * time.Second

// Identity describes a running daemon, as reported by the "identify" command.
// Legacy is true when the daemon answered but did not understand "identify"
// (a pop daemon predating the handshake protocol) — such a daemon is always
// treated as stale and reaped. ExeMod is the running binary's mtime (unix);
// it is the comparison key for "is the running daemon older than me".
type Identity struct {
	PID     int
	ExePath string
	ExeMod  int64
	Version string
	Legacy  bool
}

// Handshake asks whoever holds addr to identify itself. A non-nil Identity
// means a pop daemon is listening (current or, if Legacy, pre-protocol). A
// non-nil error means nobody is reachable as a pop daemon — the port is free,
// or held by a process that does not speak our protocol.
func Handshake(addr string) (*Identity, error) {
	resp, err := SendRequest(addr, Request{Cmd: "identify"})
	if err != nil {
		return nil, err
	}
	return &Identity{
		PID:     resp.PID,
		ExePath: resp.ExePath,
		ExeMod:  resp.ExeMod,
		Version: resp.Version,
		Legacy:  !resp.OK, // OK=false ⇒ "unknown command: identify" ⇒ old daemon
	}, nil
}

// SendShutdown asks the daemon at addr to exit gracefully.
func SendShutdown(addr string) error {
	resp, err := SendRequest(addr, Request{Cmd: "shutdown"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon refused shutdown: %s", resp.Error)
	}
	return nil
}

// SendRequest dials the daemon TCP address, sends req, and returns the response.
func SendRequest(addr string, req Request) (Response, error) {
	conn, err := net.DialTimeout("tcp", addr, clientTimeout)
	if err != nil {
		return Response{}, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(clientTimeout))

	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return Response{}, fmt.Errorf("write request: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Response{}, fmt.Errorf("read response: %w", err)
		}
		return Response{}, fmt.Errorf("read response: connection closed")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}
