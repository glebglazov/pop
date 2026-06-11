package monitor

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultAddrWith(t *testing.T) {
	// POP_MONITOR_ADDR wins when set.
	t.Run("uses POP_MONITOR_ADDR when set", func(t *testing.T) {
		d := &Deps{FS: &mockFS{getenv: func(key string) string {
			if key == "POP_MONITOR_ADDR" {
				return "0.0.0.0:12345"
			}
			return ""
		}}}
		if got := DefaultAddrWith(d); got != "0.0.0.0:12345" {
			t.Errorf("got %q, want %q", got, "0.0.0.0:12345")
		}
	})

	// With no env override, falls back to the data-dir-derived address.
	t.Run("falls back to derived address when env empty", func(t *testing.T) {
		d := &Deps{FS: &mockFS{getenv: func(string) string { return "" }}}
		got := DefaultAddrWith(d)
		if got != DerivedAddrWith(d) {
			t.Errorf("got %q, want derived %q", got, DerivedAddrWith(d))
		}
	})
}

func TestDerivedAddrWith(t *testing.T) {
	// Deterministic for a given data dir, and distinct data dirs map to
	// distinct ports (ADR 0021 — this is what prevents cross-instance
	// collisions). Loopback host, port in the dynamic range.
	mk := func(xdg string) *Deps {
		return &Deps{FS: &mockFS{getenv: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return xdg
			}
			return ""
		}}}
	}

	a1 := DerivedAddrWith(mk("/tmp/dirA"))
	a1again := DerivedAddrWith(mk("/tmp/dirA"))
	a2 := DerivedAddrWith(mk("/tmp/dirB"))

	if a1 != a1again {
		t.Errorf("not deterministic: %q vs %q", a1, a1again)
	}
	if a1 == a2 {
		t.Errorf("distinct data dirs collided on %q", a1)
	}
	host, portStr, err := net.SplitHostPort(a1)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", a1, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("host = %q, want loopback", host)
	}
	if p, _ := net.LookupPort("tcp", portStr); p < derivedPortBase {
		t.Errorf("port %s below dynamic-range base %d", portStr, derivedPortBase)
	}
}

// startTestServer spins up ListenAndServe on an ephemeral port and returns
// the actual bound address. Cleanup closes the listener.
func startTestServer(t *testing.T, handler RequestHandler) string {
	t.Helper()
	ln, err := ListenAndServe("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func TestHandshake(t *testing.T) {
	t.Run("current daemon returns identity", func(t *testing.T) {
		addr := startTestServer(t, func(req Request) Response {
			if req.Cmd != "identify" {
				return Response{OK: false, Error: "unexpected"}
			}
			return Response{OK: true, PID: 4242, ExePath: "/bin/pop", ExeMod: 99, Version: "test"}
		})
		id, err := Handshake(addr)
		if err != nil {
			t.Fatalf("Handshake: %v", err)
		}
		if id.Legacy {
			t.Error("Legacy = true, want false for a current daemon")
		}
		if id.PID != 4242 || id.ExeMod != 99 {
			t.Errorf("identity = %+v, want PID 4242 ExeMod 99", id)
		}
	})

	t.Run("pre-protocol daemon flagged Legacy", func(t *testing.T) {
		// An old daemon does not know "identify" → replies OK=false.
		addr := startTestServer(t, func(req Request) Response {
			return Response{OK: false, Error: "unknown command: " + req.Cmd}
		})
		id, err := Handshake(addr)
		if err != nil {
			t.Fatalf("Handshake: %v", err)
		}
		if !id.Legacy {
			t.Error("Legacy = false, want true for a pre-protocol daemon")
		}
	})

	t.Run("nobody listening returns error", func(t *testing.T) {
		// Reserve a port, close it, then handshake the now-free address.
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := ln.Addr().String()
		ln.Close()
		if _, err := Handshake(addr); err == nil {
			t.Error("expected error handshaking a free port")
		}
	})
}

func TestSendShutdown(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		addr := startTestServer(t, func(Request) Response { return Response{OK: true} })
		if err := SendShutdown(addr); err != nil {
			t.Errorf("SendShutdown: %v", err)
		}
	})
	t.Run("refused", func(t *testing.T) {
		addr := startTestServer(t, func(Request) Response { return Response{OK: false, Error: "busy"} })
		if err := SendShutdown(addr); err == nil {
			t.Error("expected error when daemon refuses shutdown")
		}
	})
}

// TestRunDaemonWith_AddrInUse verifies the bind-ordering fix: a daemon that
// loses the bind race returns ErrAddrInUse and does NOT touch the PID file
// (the loser must not delete the winner's liveness marker).
func TestRunDaemonWith_AddrInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "monitor.pid")
	statePath := filepath.Join(dir, "monitor.json")

	err = RunDaemonWith(DefaultDeps(), statePath, pidPath, addr,
		func(Request) Response { return Response{OK: true} })

	if !errors.Is(err, ErrAddrInUse) {
		t.Fatalf("err = %v, want ErrAddrInUse", err)
	}
	if _, statErr := os.Stat(pidPath); statErr == nil {
		t.Error("PID file was written/left by a daemon that failed to bind")
	}
}

func TestSocketRoundTrip(t *testing.T) {
	handler := func(req Request) Response {
		if req.Cmd != "set-status" {
			return Response{OK: false, Error: "unknown cmd"}
		}
		return Response{OK: true}
	}

	addr := startTestServer(t, handler)

	req := Request{
		Cmd:    "set-status",
		PaneID: "%42",
		Status: "working",
	}
	resp, err := SendRequest(addr, req)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if !resp.OK {
		t.Errorf("resp.OK = false, error: %s", resp.Error)
	}
}

func TestSocketRoundTrip_HandlerError(t *testing.T) {
	handler := func(req Request) Response {
		return Response{OK: false, Error: "something broke"}
	}

	addr := startTestServer(t, handler)

	resp, err := SendRequest(addr, Request{Cmd: "set-status", PaneID: "%1", Status: "working"})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if resp.OK {
		t.Error("expected resp.OK = false")
	}
	if resp.Error != "something broke" {
		t.Errorf("resp.Error = %q, want %q", resp.Error, "something broke")
	}
}

func TestSocketRoundTrip_AllFields(t *testing.T) {
	var received Request
	handler := func(req Request) Response {
		received = req
		return Response{OK: true}
	}

	addr := startTestServer(t, handler)

	req := Request{
		Cmd:        "set-status",
		PaneID:     "%99",
		Status:     "unread",
		Source:     "tmux-global",
		NoRegister: true,
	}
	_, err := SendRequest(addr, req)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}

	if received.PaneID != "%99" {
		t.Errorf("PaneID = %q, want %%99", received.PaneID)
	}
	if received.Status != "unread" {
		t.Errorf("Status = %q, want unread", received.Status)
	}
	if received.Source != "tmux-global" {
		t.Errorf("Source = %q, want tmux-global", received.Source)
	}
	if !received.NoRegister {
		t.Error("NoRegister = false, want true")
	}
}

func TestSocketMultipleRequests(t *testing.T) {
	count := 0
	handler := func(req Request) Response {
		count++
		return Response{OK: true}
	}

	addr := startTestServer(t, handler)

	for i := range 10 {
		resp, err := SendRequest(addr, Request{
			Cmd:    "set-status",
			PaneID: "%1",
			Status: "working",
		})
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if !resp.OK {
			t.Fatalf("request %d: not OK", i)
		}
	}

	if count != 10 {
		t.Errorf("handler called %d times, want 10", count)
	}
}

func TestSendRequest_NoServer(t *testing.T) {
	// Grab an ephemeral port then close it, so the address is guaranteed
	// unbound for the duration of the test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	_, err = SendRequest(addr, Request{Cmd: "set-status"})
	if err == nil {
		t.Fatal("expected error when no server is listening")
	}
}

func TestSendRequest_Timeout(t *testing.T) {
	// Accept but never respond — client should hit its read deadline.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		time.Sleep(10 * time.Second)
		conn.Close()
	}()

	start := time.Now()
	_, err = SendRequest(ln.Addr().String(), Request{Cmd: "set-status", PaneID: "%1", Status: "working"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took %v, expected ~2s", elapsed)
	}
}

// mockFS is a minimal mock for DefaultAddrWith tests.
type mockFS struct {
	getenv      func(string) string
	userHomeDir func() (string, error)
}

func (m *mockFS) Getwd() (string, error) { return "", nil }
func (m *mockFS) UserHomeDir() (string, error) {
	if m.userHomeDir == nil {
		return "", nil
	}
	return m.userHomeDir()
}
func (m *mockFS) Getenv(key string) string                    { return m.getenv(key) }
func (m *mockFS) Stat(string) (os.FileInfo, error)            { return nil, nil }
func (m *mockFS) ReadDir(string) ([]os.DirEntry, error)       { return nil, nil }
func (m *mockFS) ReadFile(string) ([]byte, error)             { return nil, nil }
func (m *mockFS) WriteFile(string, []byte, os.FileMode) error { return nil }
func (m *mockFS) MkdirAll(string, os.FileMode) error          { return nil }
func (m *mockFS) Rename(string, string) error                 { return nil }
func (m *mockFS) RemoveAll(string) error                      { return nil }
func (m *mockFS) DirFS(string) fs.FS                          { return nil }
func (m *mockFS) EvalSymlinks(string) (string, error)         { return "", nil }
