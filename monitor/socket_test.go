package monitor

import (
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultSocketPathWith(t *testing.T) {
	tests := []struct {
		name     string
		xdgData  string
		userHome string
		expected string
	}{
		{
			name:     "uses XDG_DATA_HOME when set",
			xdgData:  "/custom/data",
			userHome: "/home/user",
			expected: "/custom/data/pop/run/pop.sock",
		},
		{
			name:     "falls back to ~/.local/share when XDG not set",
			xdgData:  "",
			userHome: "/home/user",
			expected: "/home/user/.local/share/pop/run/pop.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &mockFS{
					getenv: func(key string) string {
						if key == "XDG_DATA_HOME" {
							return tt.xdgData
						}
						return ""
					},
					userHomeDir: func() (string, error) {
						return tt.userHome, nil
					},
				},
			}
			result := DefaultSocketPathWith(d)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSocketRoundTrip(t *testing.T) {
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "test.sock")

	handler := func(req Request) Response {
		if req.Cmd != "set-status" {
			return Response{OK: false, Error: "unknown cmd"}
		}
		return Response{OK: true}
	}

	ln, err := ListenAndServe(sockPath, handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer ln.Close()

	req := Request{
		Cmd:    "set-status",
		PaneID: "%42",
		Status: "working",
	}
	resp, err := SendRequest(sockPath, req)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if !resp.OK {
		t.Errorf("resp.OK = false, error: %s", resp.Error)
	}
}

func TestSocketRoundTrip_HandlerError(t *testing.T) {
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "test.sock")

	handler := func(req Request) Response {
		return Response{OK: false, Error: "something broke"}
	}

	ln, err := ListenAndServe(sockPath, handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer ln.Close()

	resp, err := SendRequest(sockPath, Request{Cmd: "set-status", PaneID: "%1", Status: "working"})
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
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "test.sock")

	var received Request
	handler := func(req Request) Response {
		received = req
		return Response{OK: true}
	}

	ln, err := ListenAndServe(sockPath, handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer ln.Close()

	req := Request{
		Cmd:        "set-status",
		PaneID:     "%99",
		Status:     "unread",
		Source:     "tmux-global",
		NoRegister: true,
	}
	_, err = SendRequest(sockPath, req)
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
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "test.sock")

	count := 0
	handler := func(req Request) Response {
		count++
		return Response{OK: true}
	}

	ln, err := ListenAndServe(sockPath, handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer ln.Close()

	for i := range 10 {
		resp, err := SendRequest(sockPath, Request{
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

func TestSendRequest_NoSocket(t *testing.T) {
	_, err := SendRequest("/nonexistent/pop.sock", Request{Cmd: "set-status"})
	if err == nil {
		t.Fatal("expected error when socket doesn't exist")
	}
}

func TestCleanStaleSocket(t *testing.T) {
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "stale.sock")

	// Create a stale socket file (not listening)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	// Socket file still exists but nobody is listening

	err = cleanStaleSocket(sockPath)
	if err != nil {
		t.Fatalf("cleanStaleSocket: %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("stale socket file should have been removed")
	}
}

func TestCleanStaleSocket_ActiveSocket(t *testing.T) {
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "active.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	err = cleanStaleSocket(sockPath)
	if err == nil {
		t.Fatal("expected error when socket is active")
	}
}

func TestCleanStaleSocket_NoFile(t *testing.T) {
	err := cleanStaleSocket("/nonexistent/pop.sock")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent socket, got: %v", err)
	}
}

func TestListenAndServe_CreatesDirectory(t *testing.T) {
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "nested", "dir", "pop.sock")

	handler := func(req Request) Response { return Response{OK: true} }

	ln, err := ListenAndServe(sockPath, handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer ln.Close()

	if _, err := os.Stat(filepath.Dir(sockPath)); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestSendRequest_Timeout(t *testing.T) {
	dir := shortSocketDir(t)
	sockPath := filepath.Join(dir, "slow.sock")

	// Create a listener that accepts but never responds
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Hold connection open, never respond
		time.Sleep(10 * time.Second)
		conn.Close()
	}()

	start := time.Now()
	_, err = SendRequest(sockPath, Request{Cmd: "set-status", PaneID: "%1", Status: "working"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took %v, expected ~2s", elapsed)
	}
}

// shortSocketDir creates a temp directory with a short path to stay within
// the 108-char unix socket path limit on macOS. t.TempDir() paths include
// the full test name which can easily exceed the limit.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// mockFS is a minimal mock for DefaultSocketPathWith tests.
type mockFS struct {
	getenv      func(string) string
	userHomeDir func() (string, error)
}

func (m *mockFS) Getwd() (string, error)                              { return "", nil }
func (m *mockFS) UserHomeDir() (string, error)                        { return m.userHomeDir() }
func (m *mockFS) Getenv(key string) string                            { return m.getenv(key) }
func (m *mockFS) Stat(string) (os.FileInfo, error)                    { return nil, nil }
func (m *mockFS) ReadDir(string) ([]os.DirEntry, error)               { return nil, nil }
func (m *mockFS) ReadFile(string) ([]byte, error)                     { return nil, nil }
func (m *mockFS) WriteFile(string, []byte, os.FileMode) error         { return nil }
func (m *mockFS) MkdirAll(string, os.FileMode) error                  { return nil }
func (m *mockFS) DirFS(string) fs.FS                                  { return nil }
func (m *mockFS) EvalSymlinks(string) (string, error)                 { return "", nil }
