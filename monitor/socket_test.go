package monitor

import (
	"io/fs"
	"net"
	"os"
	"testing"
	"time"
)

func TestDefaultAddrWith(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		expected string
	}{
		{
			name:     "uses POP_MONITOR_ADDR when set",
			envVal:   "0.0.0.0:12345",
			expected: "0.0.0.0:12345",
		},
		{
			name:     "falls back to default when env empty",
			envVal:   "",
			expected: defaultMonitorAddr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &mockFS{
					getenv: func(key string) string {
						if key == "POP_MONITOR_ADDR" {
							return tt.envVal
						}
						return ""
					},
				},
			}
			result := DefaultAddrWith(d)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
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
func (m *mockFS) DirFS(string) fs.FS                          { return nil }
func (m *mockFS) EvalSymlinks(string) (string, error)         { return "", nil }
