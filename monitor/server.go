package monitor

import (
	"bufio"
	"encoding/json"
	"net"

	"github.com/glebglazov/pop/debug"
)

// Request represents a command sent over the TCP socket.
type Request struct {
	Cmd        string `json:"cmd"`
	PaneID     string `json:"pane_id"`
	Status     string `json:"status"`
	Source     string `json:"source,omitempty"`
	NoRegister bool   `json:"no_register,omitempty"`
}

// Response is the daemon's reply to a request.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// RequestHandler processes a single request and returns a response.
type RequestHandler func(req Request) Response

// ListenAndServe binds a TCP listener at addr and dispatches incoming
// requests to handler. Returns the listener so the caller can close it
// during shutdown. The accept loop runs in a background goroutine.
func ListenAndServe(addr string, handler RequestHandler) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
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

	remote := conn.RemoteAddr().String()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			debug.Log("tcp %s: scan error: %v", remote, err)
		} else {
			debug.Log("tcp %s: empty request", remote)
		}
		return
	}

	raw := scanner.Bytes()
	debug.Log("tcp %s: recv %d bytes: %s", remote, len(raw), raw)

	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		debug.Error("tcp %s: unmarshal request: %v (payload=%q)", remote, err, raw)
		writeResponse(conn, Response{OK: false, Error: "invalid request"})
		return
	}

	debug.Log("tcp %s: req cmd=%s pane=%s status=%s source=%s no_register=%v",
		remote, req.Cmd, req.PaneID, req.Status, req.Source, req.NoRegister)

	resp := handler(req)
	debug.Log("tcp %s: resp ok=%v err=%q", remote, resp.OK, resp.Error)
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
