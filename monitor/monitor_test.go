package monitor

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestDefaultStatePathWith(t *testing.T) {
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
			expected: "/custom/data/pop/monitor.json",
		},
		{
			name:     "falls back to ~/.local/share when XDG not set",
			xdgData:  "",
			userHome: "/home/user",
			expected: "/home/user/.local/share/pop/monitor.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					GetenvFunc: func(key string) string {
						if key == "XDG_DATA_HOME" {
							return tt.xdgData
						}
						return ""
					},
					UserHomeDirFunc: func() (string, error) {
						return tt.userHome, nil
					},
				},
			}

			result := DefaultStatePathWith(d)
			if result != tt.expected {
				t.Errorf("DefaultStatePathWith() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDefaultPIDPathWith(t *testing.T) {
	d := &Deps{
		FS: &deps.MockFileSystem{
			GetenvFunc: func(key string) string {
				if key == "XDG_DATA_HOME" {
					return "/custom/data"
				}
				return ""
			},
		},
	}

	result := DefaultPIDPathWith(d)
	expected := "/custom/data/pop/monitor.pid"
	if result != expected {
		t.Errorf("DefaultPIDPathWith() = %q, want %q", result, expected)
	}
}

func TestLoadWith(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		fileErr   error
		wantPanes int
		wantErr   bool
	}{
		{
			name:      "returns empty state when file not found",
			fileErr:   os.ErrNotExist,
			wantPanes: 0,
		},
		{
			name:      "returns empty state on parse error",
			content:   "invalid json",
			wantPanes: 0,
		},
		{
			name:    "returns error on read error",
			fileErr: os.ErrPermission,
			wantErr: true,
		},
		{
			name:      "loads existing state",
			content:   `{"panes":{"%5":{"pane_id":"%5","session":"myproject","status":"working","updated_at":"2024-01-01T00:00:00Z"}}}`,
			wantPanes: 1,
		},
		{
			name:      "handles null panes field",
			content:   `{"panes":null}`,
			wantPanes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					ReadFileFunc: func(path string) ([]byte, error) {
						if tt.fileErr != nil {
							return nil, tt.fileErr
						}
						return []byte(tt.content), nil
					},
				},
			}

			s, err := LoadWith(d, "/test/monitor.json")

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadWith() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(s.Panes) != tt.wantPanes {
				t.Errorf("got %d panes, want %d", len(s.Panes), tt.wantPanes)
			}
		})
	}
}

func TestLoadWith_RoundTrip(t *testing.T) {
	var savedData []byte

	d := &Deps{
		FS: &deps.MockFileSystem{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedData = data
				return nil
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return savedData, nil
			},
		},
	}

	// Create and save state
	s := &State{
		Panes: map[string]*PaneEntry{
			"%5": {
				PaneID:  "%5",
				Session: "myproject",
				Status:  StatusWorking,
			},
		},
		path: "/test/monitor.json",
	}

	if err := s.SaveWith(d); err != nil {
		t.Fatalf("SaveWith() error = %v", err)
	}

	// Reload and verify
	loaded, err := LoadWith(d, "/test/monitor.json")
	if err != nil {
		t.Fatalf("LoadWith() error = %v", err)
	}

	if len(loaded.Panes) != 1 {
		t.Fatalf("got %d panes, want 1", len(loaded.Panes))
	}

	entry := loaded.Panes["%5"]
	if entry == nil {
		t.Fatal("pane %5 not found after round-trip")
	}
	if entry.Session != "myproject" {
		t.Errorf("session = %q, want %q", entry.Session, "myproject")
	}
	if entry.Status != StatusWorking {
		t.Errorf("status = %q, want %q", entry.Status, StatusWorking)
	}
}

func TestFollowingFieldRoundTrip(t *testing.T) {
	var savedData []byte

	d := &Deps{
		FS: &deps.MockFileSystem{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedData = data
				return nil
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return savedData, nil
			},
		},
	}

	s := &State{
		Panes: map[string]*PaneEntry{
			"%5": {PaneID: "%5", Session: "proj", Status: StatusWorking, Following: true},
			"%6": {PaneID: "%6", Session: "proj2", Status: StatusRead, Following: false},
		},
		path: "/test/monitor.json",
	}

	if err := s.SaveWith(d); err != nil {
		t.Fatalf("SaveWith() error = %v", err)
	}

	loaded, err := LoadWith(d, "/test/monitor.json")
	if err != nil {
		t.Fatalf("LoadWith() error = %v", err)
	}

	if !loaded.Panes["%5"].Following {
		t.Error("pane %5: Following should be true after round-trip")
	}
	if loaded.Panes["%6"].Following {
		t.Error("pane %6: Following should be false after round-trip")
	}
}

func TestFollowingFieldBackwardCompat(t *testing.T) {
	// JSON without "following" field should default to false
	jsonData := `{"panes":{"%5":{"pane_id":"%5","session":"proj","status":"working","updated_at":"2024-01-01T00:00:00Z"}}}`

	d := &Deps{
		FS: &deps.MockFileSystem{
			ReadFileFunc: func(path string) ([]byte, error) {
				return []byte(jsonData), nil
			},
		},
	}

	loaded, err := LoadWith(d, "/test/monitor.json")
	if err != nil {
		t.Fatalf("LoadWith() error = %v", err)
	}

	if loaded.Panes["%5"].Following {
		t.Error("pane %5: Following should default to false for old JSON")
	}
}

func TestSaveWith(t *testing.T) {
	var savedData []byte
	var savedPath string

	d := &Deps{
		FS: &deps.MockFileSystem{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedPath = path
				savedData = data
				return nil
			},
		},
	}

	s := &State{
		Panes: map[string]*PaneEntry{
			"%5": {PaneID: "%5", Session: "test", Status: StatusWorking},
		},
		path: "/test/dir/monitor.json",
	}

	if err := s.SaveWith(d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if savedPath != "/test/dir/monitor.json" {
		t.Errorf("saved to wrong path: %s", savedPath)
	}

	if !strings.Contains(string(savedData), `"pane_id": "%5"`) {
		t.Error("saved data doesn't contain expected pane_id")
	}
}

func TestSessionsNeedingAttention(t *testing.T) {
	s := &State{
		Panes: map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "project-a", Status: StatusNeedsAttention},
			"%2": {PaneID: "%2", Session: "project-a", Status: StatusWorking},
			"%3": {PaneID: "%3", Session: "project-b", Status: StatusWorking},
			"%4": {PaneID: "%4", Session: "project-c", Status: StatusNeedsAttention},
			"%5": {PaneID: "%5", Session: "project-d", Status: StatusUnknown},
		},
	}

	result := s.SessionsNeedingAttention()

	if len(result) != 2 {
		t.Fatalf("got %d sessions, want 2", len(result))
	}
	if !result["project-a"] {
		t.Error("expected project-a to need attention")
	}
	if !result["project-c"] {
		t.Error("expected project-c to need attention")
	}
	if result["project-b"] {
		t.Error("expected project-b to NOT need attention")
	}
}

func TestSessionsNeedingAttention_Empty(t *testing.T) {
	s := &State{Panes: make(map[string]*PaneEntry)}
	result := s.SessionsNeedingAttention()
	if len(result) != 0 {
		t.Errorf("got %d sessions, want 0", len(result))
	}
}

func TestPanesNeedingAttention(t *testing.T) {
	tests := []struct {
		name     string
		panes    map[string]*PaneEntry
		expected int
	}{
		{
			name: "filters only needs_attention",
			panes: map[string]*PaneEntry{
				"%1": {PaneID: "%1", Status: StatusNeedsAttention},
				"%2": {PaneID: "%2", Status: StatusWorking},
				"%3": {PaneID: "%3", Status: StatusRead},
				"%4": {PaneID: "%4", Status: StatusNeedsAttention},
			},
			expected: 2,
		},
		{
			name:     "empty panes",
			panes:    map[string]*PaneEntry{},
			expected: 0,
		},
		{
			name:     "nil panes",
			panes:    nil,
			expected: 0,
		},
		{
			name: "no attention panes",
			panes: map[string]*PaneEntry{
				"%1": {PaneID: "%1", Status: StatusWorking},
				"%2": {PaneID: "%2", Status: StatusRead},
			},
			expected: 0,
		},
		{
			name: "all attention",
			panes: map[string]*PaneEntry{
				"%1": {PaneID: "%1", Status: StatusNeedsAttention},
				"%2": {PaneID: "%2", Status: StatusNeedsAttention},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{Panes: tt.panes}
			result := s.PanesNeedingAttention()
			if len(result) != tt.expected {
				t.Errorf("got %d panes, want %d", len(result), tt.expected)
			}
			for _, p := range result {
				if p.Status != StatusNeedsAttention {
					t.Errorf("pane %s has status %s, want %s", p.PaneID, p.Status, StatusNeedsAttention)
				}
			}
		})
	}
}

func TestPanesActive(t *testing.T) {
	tests := []struct {
		name     string
		panes    map[string]*PaneEntry
		expected int
	}{
		{
			name: "filters attention and working",
			panes: map[string]*PaneEntry{
				"%1": {PaneID: "%1", Status: StatusNeedsAttention},
				"%2": {PaneID: "%2", Status: StatusWorking},
				"%3": {PaneID: "%3", Status: StatusRead},
				"%4": {PaneID: "%4", Status: StatusUnknown},
			},
			expected: 2,
		},
		{
			name:     "empty panes",
			panes:    map[string]*PaneEntry{},
			expected: 0,
		},
		{
			name: "only read and unknown",
			panes: map[string]*PaneEntry{
				"%1": {PaneID: "%1", Status: StatusRead},
				"%2": {PaneID: "%2", Status: StatusUnknown},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{Panes: tt.panes}
			result := s.PanesActive()
			if len(result) != tt.expected {
				t.Errorf("got %d panes, want %d", len(result), tt.expected)
			}
			for _, p := range result {
				if p.Status != StatusNeedsAttention && p.Status != StatusWorking {
					t.Errorf("pane %s has status %s, want attention or working", p.PaneID, p.Status)
				}
			}
		})
	}
}

func TestPanesAll(t *testing.T) {
	tests := []struct {
		name     string
		panes    map[string]*PaneEntry
		expected int
	}{
		{
			name: "returns all panes",
			panes: map[string]*PaneEntry{
				"%1": {PaneID: "%1", Status: StatusNeedsAttention},
				"%2": {PaneID: "%2", Status: StatusWorking},
				"%3": {PaneID: "%3", Status: StatusRead},
			},
			expected: 3,
		},
		{
			name:     "empty panes returns empty slice",
			panes:    map[string]*PaneEntry{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{Panes: tt.panes}
			result := s.PanesAll()
			if result == nil {
				t.Fatal("PanesAll() returned nil, want non-nil slice")
			}
			if len(result) != tt.expected {
				t.Errorf("got %d panes, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestIsDaemonRunningWith(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		fileErr  error
		expected bool
	}{
		{
			name:     "false when PID file missing",
			fileErr:  os.ErrNotExist,
			expected: false,
		},
		{
			name:     "false when PID file has invalid content",
			content:  "not-a-number",
			expected: false,
		},
		{
			name:     "false when PID file is empty",
			content:  "",
			expected: false,
		},
		{
			name:     "false when process does not exist",
			content:  "999999999",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					ReadFileFunc: func(path string) ([]byte, error) {
						if tt.fileErr != nil {
							return nil, tt.fileErr
						}
						return []byte(tt.content), nil
					},
				},
			}

			result := IsDaemonRunningWith(d, "/test/monitor.pid")
			if result != tt.expected {
				t.Errorf("IsDaemonRunningWith() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsDaemonRunningWith_CurrentProcess(t *testing.T) {
	// Current process PID should be detected as running
	pid := os.Getpid()
	d := &Deps{
		FS: &deps.MockFileSystem{
			ReadFileFunc: func(path string) ([]byte, error) {
				return []byte(fmt.Sprintf("%d", pid)), nil
			},
		},
	}

	result := IsDaemonRunningWith(d, "/test/monitor.pid")
	if !result {
		t.Errorf("IsDaemonRunningWith() = false for current PID %d, want true", pid)
	}
}
