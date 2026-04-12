package monitor

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"

	"github.com/glebglazov/pop/debug"
)

// Request represents a command sent over the unix socket.
type Request struct {
	Cmd        string `json:"cmd"`
	PaneID     string `json:"pane_id"`
	Status     string `json:"status"`
	Source     string `json:"source,omitempty"`
	NoRegister bool   `json:"no_register,omitempty"`
}

// Response is the daemon's reply to a socket request.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// RequestHandler processes a single request and returns a response.
type RequestHandler func(req Request) Response

// ListenAndServe creates a unix socket at socketPath and dispatches
// incoming requests to handler. Returns the listener so the caller
// can close it during shutdown. The accept loop runs in a background
// goroutine.
func ListenAndServe(socketPath string, handler RequestHandler) (net.Listener, error) {
	if err := cleanStaleSocket(socketPath); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return nil, err
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	go acceptLoop(ln, handler)
	return ln, nil
}

// acceptLoop runs until the listener is closed.
func acceptLoop(ln net.Listener, handler RequestHandler) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener closed — normal shutdown path.
			return
		}
		go handleConn(conn, handler)
	}
}

// handleConn reads one JSON request, dispatches it, writes the response, and closes.
func handleConn(conn net.Conn, handler RequestHandler) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}

	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		debug.Error("socket: unmarshal request: %v", err)
		writeResponse(conn, Response{OK: false, Error: "invalid request"})
		return
	}

	resp := handler(req)
	writeResponse(conn, resp)
}

func writeResponse(conn net.Conn, resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		debug.Error("socket: marshal response: %v", err)
		return
	}
	data = append(data, '\n')
	conn.Write(data)
}

// cleanStaleSocket removes a leftover socket file if no daemon owns it.
func cleanStaleSocket(socketPath string) error {
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return nil
	}
	// Socket file exists — try connecting to see if a daemon is listening.
	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		conn.Close()
		// Something is already listening — don't remove.
		return &net.OpError{Op: "listen", Net: "unix", Addr: &net.UnixAddr{Name: socketPath}, Err: os.ErrExist}
	}
	// Stale socket — remove it.
	return os.Remove(socketPath)
}
