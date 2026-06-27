package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// staleClaudeSettings is a settings.json body carrying an old-style pop hook.
// The pop marker makes the dry-run report the agent as installed; the bytes
// differ from the current serialization so it is also reported as stale. Used
// by the refresh tests now that claude's status wiring lives only in
// settings.json (no skill files on the default path).
const staleClaudeSettings = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"~/.local/bin/pop-status unread"}]}]}}`

// ----- Fake filesystem -------------------------------------------------------

// fakeFS is a tiny in-memory filesystem used to drive integrateDeps in tests.
// It records exact paths and contents so tests can assert directory layout.
type fakeFS struct {
	files     map[string][]byte
	dirs      map[string]bool
	symlinks  map[string]string // link path → target
	readErr   map[string]error  // path → error to return from readFile
	writeErr  map[string]error  // path → error to return from writeFile
	mkdirErr  map[string]error
	removeErr map[string]error
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files:     map[string][]byte{},
		dirs:      map[string]bool{},
		symlinks:  map[string]string{},
		readErr:   map[string]error{},
		writeErr:  map[string]error{},
		mkdirErr:  map[string]error{},
		removeErr: map[string]error{},
	}
}

func (f *fakeFS) writeFile(path string, data []byte, _ os.FileMode) error {
	if err := f.writeErr[path]; err != nil {
		return err
	}
	f.files[path] = append([]byte{}, data...)
	return nil
}

func (f *fakeFS) readFile(path string) ([]byte, error) {
	if err := f.readErr[path]; err != nil {
		return nil, err
	}
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *fakeFS) mkdirAll(path string, _ os.FileMode) error {
	if err := f.mkdirErr[path]; err != nil {
		return err
	}
	f.dirs[path] = true
	return nil
}

func (f *fakeFS) removeAll(path string) error {
	if err := f.removeErr[path]; err != nil {
		return err
	}
	delete(f.dirs, path)
	delete(f.symlinks, path)
	prefix := path + string(filepath.Separator)
	for k := range f.files {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(f.files, k)
		}
	}
	for k := range f.dirs {
		if strings.HasPrefix(k, prefix) {
			delete(f.dirs, k)
		}
	}
	for k := range f.symlinks {
		if strings.HasPrefix(k, prefix) {
			delete(f.symlinks, k)
		}
	}
	return nil
}

func (f *fakeFS) symlink(target, link string) error {
	f.symlinks[link] = target
	return nil
}

func (f *fakeFS) readlink(link string) (string, error) {
	target, ok := f.symlinks[link]
	if !ok {
		return "", os.ErrNotExist
	}
	return target, nil
}

// lstatMode reports the mode bits for an entry without following symlinks.
func (f *fakeFS) lstatMode(path string) (os.FileMode, error) {
	if _, ok := f.symlinks[path]; ok {
		return os.ModeSymlink, nil
	}
	if _, ok := f.files[path]; ok {
		return 0, nil
	}
	if f.dirs[path] {
		return os.ModeDir, nil
	}
	return 0, os.ErrNotExist
}

// fakeDeps wires a fakeFS into the integrateDeps shape.
func fakeDeps(home string, fs *fakeFS, stdout io.Writer) *integrateDeps {
	return &integrateDeps{
		userHomeDir: func() (string, error) { return home, nil },
		readFile:    fs.readFile,
		writeFile:   fs.writeFile,
		mkdirAll:    fs.mkdirAll,
		removeAll:   fs.removeAll,
		stdout:      stdout,
		logf:        func(string, ...any) {}, // no-op; override per-test to capture
		dataDir:     func() (string, error) { return filepath.Join(home, ".local", "share", "pop"), nil },
		symlink:     fs.symlink,
		readlink:    fs.readlink,
		lstatMode:   fs.lstatMode,
	}
}

// sortedKeys is a small helper used by failure messages so they're stable.
func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ----- isPopHook / removePopHooks --------------------------------------------

func TestIsPopHook(t *testing.T) {
	tests := []struct {
		name     string
		entry    interface{}
		expected bool
	}{
		{
			name: "pop monitor hook",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"command": "pop monitor set-status $PANE_ID working"},
				},
			},
			expected: true,
		},
		{
			name: "pop pane set-status hook",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"command": "pop pane set-status $PANE_ID needs_attention"},
				},
			},
			expected: true,
		},
		{
			name: "non-pop hook",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"command": "echo done"},
				},
			},
			expected: false,
		},
		{name: "non-map entry", entry: "not a map", expected: false},
		{
			name:     "no hooks key",
			entry:    map[string]interface{}{"other": "value"},
			expected: false,
		},
		{
			name:     "empty hooks array",
			entry:    map[string]interface{}{"hooks": []interface{}{}},
			expected: false,
		},
		{
			name: "mixed hooks only one is pop",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"command": "echo hello"},
					map[string]interface{}{"command": "pop pane set-status #{pane_id} read 2>/dev/null || true"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPopHook(tt.entry); got != tt.expected {
				t.Errorf("isPopHook() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRemovePopHooks(t *testing.T) {
	popHook := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{"command": "pop pane set-status #{pane_id} read"},
		},
	}
	otherHook := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{"command": "echo done"},
		},
	}

	tests := []struct {
		name     string
		entries  []interface{}
		expected int
	}{
		{"removes pop hooks keeps others", []interface{}{popHook, otherHook, popHook}, 1},
		{"no pop hooks returns all", []interface{}{otherHook, otherHook}, 2},
		{"all pop hooks returns empty", []interface{}{popHook}, 0},
		{"nil input", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removePopHooks(tt.entries)
			if len(result) != tt.expected {
				t.Errorf("removePopHooks() returned %d entries, want %d", len(result), tt.expected)
			}
		})
	}
}

// ----- injectFrontmatterName -------------------------------------------------

func TestInjectFrontmatterName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		comment string
	}{
		{
			name:    "no frontmatter at all",
			input:   "# Hello\nbody\n",
			want:    "---\nname: pop-pane\n---\n# Hello\nbody\n",
			comment: "should wrap content in fresh frontmatter",
		},
		{
			name:    "frontmatter without name",
			input:   "---\ndescription: a thing\n---\nbody\n",
			want:    "---\nname: pop-pane\ndescription: a thing\n---\nbody\n",
			comment: "should insert name immediately after opening ---",
		},
		{
			name:    "frontmatter with existing name",
			input:   "---\nname: old-name\ndescription: x\n---\nbody\n",
			want:    "---\nname: pop-pane\ndescription: x\n---\nbody\n",
			comment: "should replace existing name in place",
		},
		{
			name:    "frontmatter with name not first",
			input:   "---\ndescription: x\nname: old\n---\nbody\n",
			want:    "---\ndescription: x\nname: pop-pane\n---\nbody\n",
			comment: "should replace name regardless of position",
		},
		{
			name:    "malformed frontmatter (no closing fence)",
			input:   "---\ndescription: x\nstill body\n",
			want:    "---\ndescription: x\nstill body\n",
			comment: "should leave malformed input untouched",
		},
		{
			name:    "empty input",
			input:   "",
			want:    "---\nname: pop-pane\n---\n",
			comment: "should wrap an empty file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := injectFrontmatterName(tt.input, "pop-pane")
			if got != tt.want {
				t.Errorf("%s\ngot:\n%q\nwant:\n%q", tt.comment, got, tt.want)
			}
		})
	}
}

// ----- runIntegrateWith dispatcher -------------------------------------------

func TestRunIntegrateWith_UnknownAgent(t *testing.T) {
	fs := newFakeFS()
	err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "vscode")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("error message should mention unknown agent, got %q", err.Error())
	}
}

func TestRunIntegrateWith_AgentNameIsCaseInsensitive(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "Claude"); err != nil {
		t.Errorf("expected case-insensitive agent matching, got error: %v", err)
	}
}

// ----- integrateClaude: directory structure ----------------------------------

func TestIntegrateClaude_WritesOnlyStatusWiring(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// settings.json (the status wiring) should be written.
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	if _, ok := fs.files[settingsPath]; !ok {
		t.Errorf("expected settings.json at %s", settingsPath)
	}

	// The default integrate path installs no skill files: nothing should land
	// under ~/.claude/commands/.
	commandsDir := filepath.Join("/h", ".claude", "commands")
	for path := range fs.files {
		if strings.HasPrefix(path, commandsDir+string(filepath.Separator)) {
			t.Errorf("default integrate path wrote a skill file: %s", path)
		}
	}
}

func TestIntegrateClaude_DoesNotWriteOutsideClaudeTree(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for path := range fs.files {
		if !strings.HasPrefix(path, "/h/.claude/") {
			t.Errorf("claude integration wrote a file outside ~/.claude: %s", path)
		}
	}
}

func TestIntegrateClaude_FreshSettings(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	var settings map[string]interface{}
	if err := json.Unmarshal(fs.files[settingsPath], &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing hooks key in settings")
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "Stop", "Notification"} {
		entries, ok := hooks[event].([]interface{})
		if !ok || len(entries) == 0 {
			t.Errorf("missing hooks for event %q", event)
		}
	}
}

func TestIntegrateClaude_PreservesExistingHooks(t *testing.T) {
	fs := newFakeFS()
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	existing := map[string]interface{}{
		"customKey": "customValue",
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "echo user hook"},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[settingsPath] = raw

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(fs.files[settingsPath], &settings)

	if settings["customKey"] != "customValue" {
		t.Error("customKey was not preserved")
	}
	hooks := settings["hooks"].(map[string]interface{})
	entries := hooks["UserPromptSubmit"].([]interface{})
	if len(entries) < 2 {
		t.Errorf("expected user hook + pop hook on UserPromptSubmit, got %d entries", len(entries))
	}
}

func TestIntegrateClaude_ReplacesOldPopHooks(t *testing.T) {
	fs := newFakeFS()
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "pop pane set-status needs_attention 2>/dev/null || true",
						},
					},
				},
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "echo keep me"},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[settingsPath] = raw

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(fs.files[settingsPath], &settings)

	hooks := settings["hooks"].(map[string]interface{})
	stopHooks := hooks["Stop"].([]interface{})
	popCount, userCount := 0, 0
	for _, h := range stopHooks {
		if isPopHook(h) {
			popCount++
		} else {
			userCount++
		}
	}
	if userCount != 1 {
		t.Errorf("expected 1 user hook preserved, got %d", userCount)
	}
	if popCount != 1 {
		t.Errorf("expected exactly 1 freshly installed pop hook, got %d", popCount)
	}
}

func TestIntegrateClaude_RemovesStaleEventKeys(t *testing.T) {
	// A previously installed pop hook on an event we no longer manage
	// (here: PostToolUse) should be cleaned up entirely on re-install,
	// not left as a null/empty entry.
	fs := newFakeFS()
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "pop pane set-status working 2>/dev/null || true",
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[settingsPath] = raw

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(fs.files[settingsPath], &settings)
	hooks := settings["hooks"].(map[string]interface{})
	if val, exists := hooks["PostToolUse"]; exists {
		t.Errorf("expected PostToolUse to be deleted, got %v", val)
	}
}

func TestIntegrateClaude_WriteError(t *testing.T) {
	fs := newFakeFS()
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	fs.writeErr[settingsPath] = os.ErrPermission

	err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude")
	if err == nil {
		t.Fatal("expected error from settings write failure")
	}
}

// claudeUserPromptHooks returns the UserPromptSubmit hook entries from the
// freshly written claude settings.json, with each entry's inner command.
func claudeUserPromptCommands(t *testing.T, fs *fakeFS) []string {
	t.Helper()
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	var settings map[string]interface{}
	if err := json.Unmarshal(fs.files[settingsPath], &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing hooks key in settings")
	}
	entries, _ := hooks["UserPromptSubmit"].([]interface{})
	var cmds []string
	for _, e := range entries {
		em, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		inner, _ := em["hooks"].([]interface{})
		for _, h := range inner {
			hm, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if c, _ := hm["command"].(string); c != "" {
				cmds = append(cmds, c)
			}
		}
	}
	return cmds
}

func countContains(cmds []string, needle string) int {
	n := 0
	for _, c := range cmds {
		if strings.Contains(c, needle) {
			n++
		}
	}
	return n
}

// The topic hook installs as a separate UserPromptSubmit entry alongside the
// set-status working hook, with no extra opt-in (ADR 0023).
func TestIntegrateClaude_InstallsTopicHook(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmds := claudeUserPromptCommands(t, fs)
	if got := countContains(cmds, "pop pane set-topic --derive"); got != 1 {
		t.Errorf("expected exactly 1 set-topic --derive UserPromptSubmit hook, got %d (cmds=%v)", got, cmds)
	}
	// The set-status working hook must remain a distinct, untouched entry.
	if got := countContains(cmds, "pop pane set-status working"); got != 1 {
		t.Errorf("expected exactly 1 set-status working UserPromptSubmit hook, got %d (cmds=%v)", got, cmds)
	}
	if len(cmds) != 2 {
		t.Errorf("expected 2 separate UserPromptSubmit entries, got %d (cmds=%v)", len(cmds), cmds)
	}
}

// Re-running integrate is idempotent: the topic hook is not duplicated.
func TestIntegrateClaude_TopicHookIdempotent(t *testing.T) {
	fs := newFakeFS()
	for i := 0; i < 3; i++ {
		if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
	}
	cmds := claudeUserPromptCommands(t, fs)
	if got := countContains(cmds, "pop pane set-topic --derive"); got != 1 {
		t.Errorf("expected exactly 1 set-topic hook after repeated installs, got %d", got)
	}
	if got := countContains(cmds, "pop pane set-status working"); got != 1 {
		t.Errorf("expected exactly 1 set-status working hook after repeated installs, got %d", got)
	}
}

// statusWiringInstalled and refresh detection see the topic hook as pop wiring,
// and removal strips it cleanly alongside the rest of the status wiring.
func TestIntegrateClaude_RemovesTopicHook(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps("/h", fs, io.Discard)
	if err := runIntegrateWith(d, "claude"); err != nil {
		t.Fatalf("install: unexpected error: %v", err)
	}

	installed, err := statusWiringInstalled(d, "/h", "claude")
	if err != nil {
		t.Fatalf("statusWiringInstalled: %v", err)
	}
	if !installed {
		t.Fatal("expected status wiring (incl. topic hook) to report installed")
	}

	if err := removeStatusWiring(d, "/h", "claude"); err != nil {
		t.Fatalf("remove: unexpected error: %v", err)
	}

	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	var settings map[string]interface{}
	json.Unmarshal(fs.files[settingsPath], &settings)
	hooks, _ := settings["hooks"].(map[string]interface{})
	for event, val := range hooks {
		entries, _ := val.([]interface{})
		for _, e := range entries {
			if isPopHook(e) {
				t.Errorf("event %q still has a pop hook after removal: %v", event, e)
			}
		}
	}
}

// A binary change re-renders the topic hook through the refresh path: an
// install missing the topic hook is detected as changed and re-installed.
func TestIntegrateClaude_RefreshRendersTopicHook(t *testing.T) {
	fs := newFakeFS()
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	// Simulate an older install that wired set-status but not the topic hook.
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "pop pane set-status working 2>/dev/null || true",
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[settingsPath] = raw

	newReal := func() *integrateDeps { return fakeDeps("/h", fs, io.Discard) }
	newDry := func() *integrateDeps { return withDryRun(fakeDeps("/h", fs, io.Discard)) }

	updated, warning := refreshStatusWiring(newDry, newReal, "claude")
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if !updated {
		t.Fatal("expected refresh to update claude status wiring (topic hook added)")
	}
	cmds := claudeUserPromptCommands(t, fs)
	if got := countContains(cmds, "pop pane set-topic --derive"); got != 1 {
		t.Errorf("expected topic hook present after refresh, got %d (cmds=%v)", got, cmds)
	}
}

func TestIsPopHookCommand_Topic(t *testing.T) {
	if !isPopHookCommand("pop pane set-topic --derive 2>/dev/null || true") {
		t.Error("expected set-topic command to be recognised as a pop hook")
	}
}

// ----- integrateCodex: hooks.json -------------------------------------------

func TestIntegrateCodex_WritesHooksJSON(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	if _, ok := fs.files[hooksPath]; !ok {
		t.Fatalf("expected hooks.json at %s, files: %v", hooksPath, sortedKeys(fs.files))
	}
	if !fs.dirs[filepath.Dir(hooksPath)] {
		t.Errorf("expected mkdirAll of %s", filepath.Dir(hooksPath))
	}
}

func TestIntegrateCodex_FreshHooks(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	var settings map[string]interface{}
	if err := json.Unmarshal(fs.files[hooksPath], &settings); err != nil {
		t.Fatalf("failed to parse hooks.json: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing hooks key")
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Stop"} {
		entries, ok := hooks[event].([]interface{})
		if !ok || len(entries) == 0 {
			t.Errorf("missing hooks for event %q", event)
		}
	}
}

func TestIntegrateCodex_PreservesExistingHooks(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	existing := map[string]interface{}{
		"customKey": "customValue",
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "echo user hook"},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[hooksPath] = raw

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(fs.files[hooksPath], &settings)

	if settings["customKey"] != "customValue" {
		t.Error("customKey was not preserved")
	}
	hooks := settings["hooks"].(map[string]interface{})
	entries := hooks["UserPromptSubmit"].([]interface{})
	if len(entries) < 2 {
		t.Errorf("expected user hook + pop hook on UserPromptSubmit, got %d entries", len(entries))
	}
}

func TestIntegrateCodex_ReplacesOldPopHooks(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "~/.local/bin/pop-status unread",
						},
					},
				},
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "echo keep me"},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[hooksPath] = raw

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(fs.files[hooksPath], &settings)

	hooks := settings["hooks"].(map[string]interface{})
	stopHooks := hooks["Stop"].([]interface{})
	popCount, userCount := 0, 0
	for _, h := range stopHooks {
		if isPopHook(h) {
			popCount++
		} else {
			userCount++
		}
	}
	if userCount != 1 {
		t.Errorf("expected 1 user hook preserved, got %d", userCount)
	}
	if popCount != 1 {
		t.Errorf("expected exactly 1 freshly installed pop hook, got %d", popCount)
	}
}

func TestIntegrateCodex_DoesNotWriteOutsideCodexTree(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for path := range fs.files {
		if !strings.HasPrefix(path, "/h/.codex/") {
			t.Errorf("codex integration wrote a file outside ~/.codex: %s", path)
		}
	}
}

func TestIntegrateCodex_WriteError(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	fs.writeErr[hooksPath] = os.ErrPermission

	err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex")
	if err == nil {
		t.Fatal("expected error from hooks write failure")
	}
}

// nestedEventCommands returns the inner command strings for a nested-format
// (claude/codex) hook event from the JSON settings file at path.
func nestedEventCommands(t *testing.T, fs *fakeFS, path, event string) []string {
	t.Helper()
	var settings map[string]interface{}
	if err := json.Unmarshal(fs.files[path], &settings); err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	entries, _ := hooks[event].([]interface{})
	var cmds []string
	for _, e := range entries {
		em, _ := e.(map[string]interface{})
		inner, _ := em["hooks"].([]interface{})
		for _, h := range inner {
			hm, _ := h.(map[string]interface{})
			if c, _ := hm["command"].(string); c != "" {
				cmds = append(cmds, c)
			}
		}
	}
	return cmds
}

// flatEventCommands returns the command strings for a flat-format (cursor) hook
// event from the JSON settings file at path.
func flatEventCommands(t *testing.T, fs *fakeFS, path, event string) []string {
	t.Helper()
	var settings map[string]interface{}
	if err := json.Unmarshal(fs.files[path], &settings); err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	entries, _ := hooks[event].([]interface{})
	var cmds []string
	for _, e := range entries {
		em, _ := e.(map[string]interface{})
		if c, _ := em["command"].(string); c != "" {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

// The codex topic hook installs as a separate UserPromptSubmit entry alongside
// set-status, labeled for codex's payload adapter (ADR 0023).
func TestIntegrateCodex_InstallsTopicHook(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	cmds := nestedEventCommands(t, fs, hooksPath, "UserPromptSubmit")
	if got := countContains(cmds, "pop pane set-topic --derive --label codex"); got != 1 {
		t.Errorf("expected 1 codex topic hook, got %d (cmds=%v)", got, cmds)
	}
	if got := countContains(cmds, "pop pane set-status working"); got != 1 {
		t.Errorf("expected set-status working hook untouched, got %d (cmds=%v)", got, cmds)
	}
}

// Re-running integrate is idempotent: the codex topic hook is not duplicated.
func TestIntegrateCodex_TopicHookIdempotent(t *testing.T) {
	fs := newFakeFS()
	for i := 0; i < 3; i++ {
		if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "codex"); err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
	}
	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	cmds := nestedEventCommands(t, fs, hooksPath, "UserPromptSubmit")
	if got := countContains(cmds, "pop pane set-topic --derive --label codex"); got != 1 {
		t.Errorf("expected 1 codex topic hook after repeated installs, got %d", got)
	}
}

// An older codex install missing the topic hook is detected as changed and the
// topic hook is rendered through the refresh path; removal then strips it.
func TestIntegrateCodex_RefreshRendersAndRemovesTopicHook(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".codex", "hooks.json")
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "pop pane set-status working 2>/dev/null || true"},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[hooksPath] = raw

	newReal := func() *integrateDeps { return fakeDeps("/h", fs, io.Discard) }
	newDry := func() *integrateDeps { return withDryRun(fakeDeps("/h", fs, io.Discard)) }

	updated, warning := refreshStatusWiring(newDry, newReal, "codex")
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if !updated {
		t.Fatal("expected refresh to add the codex topic hook")
	}
	cmds := nestedEventCommands(t, fs, hooksPath, "UserPromptSubmit")
	if got := countContains(cmds, "pop pane set-topic --derive --label codex"); got != 1 {
		t.Errorf("expected codex topic hook after refresh, got %d (cmds=%v)", got, cmds)
	}

	if err := removeStatusWiring(fakeDeps("/h", fs, io.Discard), "/h", "codex"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cmds = nestedEventCommands(t, fs, hooksPath, "UserPromptSubmit")
	if got := countContains(cmds, "pop pane set-topic"); got != 0 {
		t.Errorf("expected topic hook removed, got %d (cmds=%v)", got, cmds)
	}
}

// ----- integratePi: directory structure --------------------------------------

func TestIntegratePi_WritesExtensionAtCorrectPath(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	extPath := filepath.Join("/h", ".pi", "agent", "extensions", "pop-status-sync.ts")
	contents, ok := fs.files[extPath]
	if !ok {
		t.Fatalf("expected extension at %s, files: %v", extPath, sortedKeys(fs.files))
	}
	if !bytes.Contains(contents, []byte(`pi.exec("pop", ["pane", "set-status"`)) {
		t.Error("extension content does not look like the pop pane-status TS file")
	}
	if !bytes.Equal(contents, piExtensionFile) {
		t.Error("extension on disk should match the embedded source byte-for-byte")
	}
}

func TestIntegratePi_WritesNoSkillFiles(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The default integrate path installs only the status-sync extension; no
	// skill directories should be created under ~/.pi/agent/skills/.
	skillsDir := filepath.Join("/h", ".pi", "agent", "skills")
	for path := range fs.files {
		if strings.HasPrefix(path, skillsDir+string(filepath.Separator)) {
			t.Errorf("default integrate path wrote a pi skill file: %s", path)
		}
	}
}

func TestIntegratePi_DoesNotWriteOutsidePiTree(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for path := range fs.files {
		if !strings.HasPrefix(path, "/h/.pi/") {
			t.Errorf("pi integration wrote a file outside ~/.pi: %s", path)
		}
	}
}

func TestIntegratePi_ExtensionWriteError(t *testing.T) {
	fs := newFakeFS()
	extPath := filepath.Join("/h", ".pi", "agent", "extensions", "pop-status-sync.ts")
	fs.writeErr[extPath] = errors.New("disk full")

	err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi")
	if err == nil {
		t.Fatal("expected error from extension write failure")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected wrapped error to mention disk full, got %v", err)
	}
}

// ----- integrateOpencode: directory structure --------------------------------

func TestIntegrateOpencode_WritesPluginAtCorrectPath(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pluginPath := filepath.Join("/h", ".config", "opencode", "plugins", "pop-status-sync.ts")
	contents, ok := fs.files[pluginPath]
	if !ok {
		t.Fatalf("expected plugin at %s, files: %v", pluginPath, sortedKeys(fs.files))
	}
	if !bytes.Contains(contents, []byte(`$`+"`"+`pop pane set-status`)) {
		t.Error("plugin content does not look like the pop status-sync TS file")
	}
	if !bytes.Equal(contents, opencodeExtensionFile) {
		t.Error("plugin on disk should match the embedded source byte-for-byte")
	}
}

func TestIntegrateOpencode_WritesNoSkillFiles(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The default integrate path installs only the status-sync plugin; no
	// agent skill markdown should land under ~/.config/opencode/agent/.
	agentDir := filepath.Join("/h", ".config", "opencode", "agent")
	for path := range fs.files {
		if strings.HasPrefix(path, agentDir+string(filepath.Separator)) {
			t.Errorf("default integrate path wrote an opencode skill file: %s", path)
		}
	}
}

func TestIntegrateOpencode_DoesNotWriteOutsideOpencodeTree(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for path := range fs.files {
		if !strings.HasPrefix(path, "/h/.config/opencode/") {
			t.Errorf("opencode integration wrote a file outside ~/.config/opencode: %s", path)
		}
	}
}

func TestIntegrateOpencode_OverwritesPriorPlugin(t *testing.T) {
	// Pre-seed a stale plugin file and verify it gets overwritten.
	fs := newFakeFS()
	pluginPath := filepath.Join("/h", ".config", "opencode", "plugins", "pop-status-sync.ts")
	fs.files[pluginPath] = []byte("old plugin content")

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Equal(fs.files[pluginPath], opencodeExtensionFile) {
		t.Error("plugin file should have been overwritten with embedded content")
	}
}

func TestIntegrateOpencode_PluginWriteError(t *testing.T) {
	fs := newFakeFS()
	pluginPath := filepath.Join("/h", ".config", "opencode", "plugins", "pop-status-sync.ts")
	fs.writeErr[pluginPath] = errors.New("disk full")

	err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode")
	if err == nil {
		t.Fatal("expected error from plugin write failure")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected wrapped error to mention disk full, got %v", err)
	}
}

func TestIntegrateOpencode_AgentNameIsCaseInsensitive(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "OpEnCoDe"); err != nil {
		t.Errorf("expected case-insensitive agent matching, got error: %v", err)
	}
}

// ----- integrateCursor: hooks.json + skills ----------------------------------

func TestIntegrateCursor_WritesHooksJSON(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	if _, ok := fs.files[hooksPath]; !ok {
		t.Fatalf("expected hooks.json at %s, files: %v", hooksPath, sortedKeys(fs.files))
	}
	if !fs.dirs[filepath.Dir(hooksPath)] {
		t.Errorf("expected mkdirAll of %s", filepath.Dir(hooksPath))
	}
}

func TestIntegrateCursor_FreshHooks(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	var settings map[string]interface{}
	if err := json.Unmarshal(fs.files[hooksPath], &settings); err != nil {
		t.Fatalf("failed to parse hooks.json: %v", err)
	}

	if settings["version"] != float64(1) {
		t.Errorf("version = %v, want 1", settings["version"])
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing hooks key")
	}
	for _, event := range []string{"sessionStart", "beforeSubmitPrompt", "preToolUse", "afterAgentResponse", "stop"} {
		entries, ok := hooks[event].([]interface{})
		if !ok || len(entries) == 0 {
			t.Errorf("missing hooks for event %q", event)
		}
	}
	firstHook := hooks["sessionStart"].([]interface{})[0].(map[string]interface{})
	if cmd := firstHook["command"].(string); !strings.Contains(cmd, "--label cursor") {
		t.Errorf("sessionStart hook = %q, want --label cursor", cmd)
	}
}

func TestIntegrateCursor_PreservesExistingHooks(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	existing := map[string]interface{}{
		"version":   1,
		"customKey": "customValue",
		"hooks": map[string]interface{}{
			"beforeSubmitPrompt": []interface{}{
				map[string]interface{}{"command": "echo user hook"},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[hooksPath] = raw

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(fs.files[hooksPath], &settings)

	if settings["customKey"] != "customValue" {
		t.Error("customKey was not preserved")
	}
	hooks := settings["hooks"].(map[string]interface{})
	entries := hooks["beforeSubmitPrompt"].([]interface{})
	if len(entries) < 2 {
		t.Errorf("expected user hook + pop hook on beforeSubmitPrompt, got %d entries", len(entries))
	}
}

func TestIntegrateCursor_ReplacesOldPopHooks(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	existing := map[string]interface{}{
		"version": 1,
		"hooks": map[string]interface{}{
			"stop": []interface{}{
				map[string]interface{}{
					"command": "~/.local/bin/pop-status unread",
				},
				map[string]interface{}{
					"command": "echo keep me",
				},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[hooksPath] = raw

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(fs.files[hooksPath], &settings)

	hooks := settings["hooks"].(map[string]interface{})
	stopHooks := hooks["stop"].([]interface{})
	popCount, userCount := 0, 0
	for _, h := range stopHooks {
		if isCursorPopHook(h) {
			popCount++
		} else {
			userCount++
		}
	}
	if userCount != 1 {
		t.Errorf("expected 1 user hook preserved, got %d", userCount)
	}
	if popCount != 1 {
		t.Errorf("expected exactly 1 freshly installed pop hook, got %d", popCount)
	}
}

func TestIntegrateCursor_DoesNotWriteOutsideCursorTree(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for path := range fs.files {
		if !strings.HasPrefix(path, "/h/.cursor/") {
			t.Errorf("cursor integration wrote a file outside ~/.cursor: %s", path)
		}
	}
}

func TestIntegrateCursor_WriteError(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	fs.writeErr[hooksPath] = os.ErrPermission

	err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor")
	if err == nil {
		t.Fatal("expected error from hooks write failure")
	}
}

func TestIntegrateCursor_WritesNoSkillFiles(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The default integrate path installs only the hooks; no skill
	// directories should be created under ~/.cursor/skills/.
	skillsDir := filepath.Join("/h", ".cursor", "skills")
	for path := range fs.files {
		if strings.HasPrefix(path, skillsDir+string(filepath.Separator)) {
			t.Errorf("default integrate path wrote a cursor skill file: %s", path)
		}
	}
}

func TestIntegrateCursor_AgentNameIsCaseInsensitive(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "CuRsOr"); err != nil {
		t.Errorf("expected case-insensitive agent matching, got error: %v", err)
	}
}

// The cursor topic hook installs as a separate beforeSubmitPrompt entry
// alongside set-status, labeled for cursor's payload adapter (ADR 0023).
func TestIntegrateCursor_InstallsTopicHook(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	cmds := flatEventCommands(t, fs, hooksPath, "beforeSubmitPrompt")
	if got := countContains(cmds, "pop pane set-topic --derive --label cursor"); got != 1 {
		t.Errorf("expected 1 cursor topic hook, got %d (cmds=%v)", got, cmds)
	}
	if got := countContains(cmds, "pop pane set-status working --label cursor"); got != 1 {
		t.Errorf("expected set-status working hook untouched, got %d (cmds=%v)", got, cmds)
	}
}

// Re-running integrate is idempotent: the cursor topic hook is not duplicated.
func TestIntegrateCursor_TopicHookIdempotent(t *testing.T) {
	fs := newFakeFS()
	for i := 0; i < 3; i++ {
		if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "cursor"); err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
	}
	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	cmds := flatEventCommands(t, fs, hooksPath, "beforeSubmitPrompt")
	if got := countContains(cmds, "pop pane set-topic --derive --label cursor"); got != 1 {
		t.Errorf("expected 1 cursor topic hook after repeated installs, got %d", got)
	}
}

// An older cursor install missing the topic hook is detected as changed and the
// topic hook is rendered through the refresh path; removal then strips it.
func TestIntegrateCursor_RefreshRendersAndRemovesTopicHook(t *testing.T) {
	fs := newFakeFS()
	hooksPath := filepath.Join("/h", ".cursor", "hooks.json")
	existing := map[string]interface{}{
		"version": 1,
		"hooks": map[string]interface{}{
			"beforeSubmitPrompt": []interface{}{
				map[string]interface{}{"command": "pop pane set-status working --label cursor 2>/dev/null || true"},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	fs.files[hooksPath] = raw

	newReal := func() *integrateDeps { return fakeDeps("/h", fs, io.Discard) }
	newDry := func() *integrateDeps { return withDryRun(fakeDeps("/h", fs, io.Discard)) }

	updated, warning := refreshStatusWiring(newDry, newReal, "cursor")
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if !updated {
		t.Fatal("expected refresh to add the cursor topic hook")
	}
	cmds := flatEventCommands(t, fs, hooksPath, "beforeSubmitPrompt")
	if got := countContains(cmds, "pop pane set-topic --derive --label cursor"); got != 1 {
		t.Errorf("expected cursor topic hook after refresh, got %d (cmds=%v)", got, cmds)
	}

	if err := removeStatusWiring(fakeDeps("/h", fs, io.Discard), "/h", "cursor"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cmds = flatEventCommands(t, fs, hooksPath, "beforeSubmitPrompt")
	if got := countContains(cmds, "pop pane set-topic"); got != 0 {
		t.Errorf("expected topic hook removed, got %d (cmds=%v)", got, cmds)
	}
}

// The pi and opencode extensions carry the topic-derivation call as part of the
// status-sync wiring they install (ADR 0023). Removing the extension file (the
// status-wiring removal path for these agents) takes the topic call with it.
func TestExtensions_DeriveTopic(t *testing.T) {
	if !bytes.Contains(piExtensionFile, []byte("set-topic --derive --label pi")) {
		t.Error("pi extension missing topic derivation call")
	}
	if !bytes.Contains(opencodeExtensionFile, []byte("set-topic --derive --label opencode")) {
		t.Error("opencode extension missing topic derivation call")
	}

	// Removal of the extension file (status-wiring removal for pi/opencode)
	// removes the topic call along with it.
	for _, tc := range []struct {
		agent string
		path  string
	}{
		{"pi", filepath.Join("/h", ".pi", "agent", "extensions", "pop-status-sync.ts")},
		{"opencode", filepath.Join("/h", ".config", "opencode", "plugins", "pop-status-sync.ts")},
	} {
		fs := newFakeFS()
		if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), tc.agent); err != nil {
			t.Fatalf("%s install: %v", tc.agent, err)
		}
		if !bytes.Contains(fs.files[tc.path], []byte("set-topic --derive")) {
			t.Errorf("%s: installed extension missing topic call", tc.agent)
		}
		if err := removeStatusWiring(fakeDeps("/h", fs, io.Discard), "/h", tc.agent); err != nil {
			t.Fatalf("%s remove: %v", tc.agent, err)
		}
		if _, ok := fs.files[tc.path]; ok {
			t.Errorf("%s: extension (incl. topic call) not removed", tc.agent)
		}
	}
}

// ----- dry-run deps (withDryRun) ---------------------------------------------

// installViaFake runs a real install against the given fake FS, used by
// dry-run tests to seed "what an installed agent looks like on disk".
func installViaFake(t *testing.T, fs *fakeFS, home, agent string) {
	t.Helper()
	if err := runIntegrateWith(fakeDeps(home, fs, io.Discard), agent); err != nil {
		t.Fatalf("seed install %s: %v", agent, err)
	}
}

func TestDryRun_NoInstallation(t *testing.T) {
	// Empty fake FS: no pop artifacts for any agent. Every dry-run should
	// report neither installed nor changed.
	for _, agent := range []string{"claude", "codex", "pi", "opencode", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			fs := newFakeFS()
			d := withDryRun(fakeDeps("/h", fs, io.Discard))
			if err := runIntegrateWith(d, agent); err != nil {
				t.Fatalf("dry-run: %v", err)
			}
			if d.installed {
				t.Errorf("expected installed=false on empty FS")
			}
			if d.changed {
				t.Errorf("expected changed=false on empty FS")
			}
			// Dry-run must not have mutated the fake FS.
			if len(fs.files) != 0 {
				t.Errorf("dry-run wrote files: %v", sortedKeys(fs.files))
			}
		})
	}
}

func TestDryRun_InstalledAndCurrent(t *testing.T) {
	// Seed the FS with a real install, then dry-run against the same FS.
	// Every agent should report installed=true, changed=false.
	for _, agent := range []string{"claude", "codex", "pi", "opencode", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			fs := newFakeFS()
			installViaFake(t, fs, "/h", agent)

			d := withDryRun(fakeDeps("/h", fs, io.Discard))
			if err := runIntegrateWith(d, agent); err != nil {
				t.Fatalf("dry-run: %v", err)
			}
			if !d.installed {
				t.Errorf("expected installed=true after seed install")
			}
			if d.changed {
				t.Errorf("expected changed=false when bytes match embedded content")
			}
		})
	}
}

func TestDryRun_InstalledAndStale(t *testing.T) {
	// Seed a real install, then corrupt one file so the dry-run should
	// detect stale content and report changed=true.
	cases := []struct {
		agent     string
		stalePath string // relative to /h
	}{
		{
			agent:     "claude",
			stalePath: filepath.Join(".claude", "settings.json"),
		},
		{
			agent:     "codex",
			stalePath: filepath.Join(".codex", "hooks.json"),
		},
		{
			agent:     "pi",
			stalePath: filepath.Join(".pi", "agent", "extensions", "pop-status-sync.ts"),
		},
		{
			agent:     "opencode",
			stalePath: filepath.Join(".config", "opencode", "plugins", "pop-status-sync.ts"),
		},
		{
			agent:     "cursor",
			stalePath: filepath.Join(".cursor", "hooks.json"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			fs := newFakeFS()
			installViaFake(t, fs, "/h", tc.agent)

			fullPath := filepath.Join("/h", tc.stalePath)
			if _, exists := fs.files[fullPath]; !exists {
				t.Fatalf("seed install did not produce %s; update the test fixture", fullPath)
			}
			switch tc.agent {
			case "claude":
				fs.files[fullPath] = []byte(staleClaudeSettings)
			case "codex":
				fs.files[fullPath] = []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"~/.local/bin/pop-status unread"}]}]}}`)
			case "cursor":
				fs.files[fullPath] = []byte(`{"version":1,"hooks":{"stop":[{"command":"~/.local/bin/pop-status unread"}]}}`)
			default:
				fs.files[fullPath] = []byte("stale bytes that differ from embedded content")
			}

			d := withDryRun(fakeDeps("/h", fs, io.Discard))
			if err := runIntegrateWith(d, tc.agent); err != nil {
				t.Fatalf("dry-run: %v", err)
			}
			if !d.installed {
				t.Errorf("expected installed=true after seed install")
			}
			if !d.changed {
				t.Errorf("expected changed=true when %s has stale bytes", tc.stalePath)
			}
		})
	}
}

func TestDryRun_ClaudeSettingsNotRewrittenWhenHooksCurrent(t *testing.T) {
	// After a real install, re-running in dry-run must report the settings
	// file as installed but unchanged. This is the critical "no formatting
	// churn" case for claude — the main reason the dry-run exists.
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	before := append([]byte{}, fs.files[settingsPath]...)

	d := withDryRun(fakeDeps("/h", fs, io.Discard))
	if err := runIntegrateWith(d, "claude"); err != nil {
		t.Fatalf("dry-run claude: %v", err)
	}

	if !d.installed {
		t.Error("expected installed=true (settings.json contains pop hooks)")
	}
	if d.changed {
		t.Error("expected changed=false when hooks are already current")
	}
	if !bytes.Equal(before, fs.files[settingsPath]) {
		t.Error("dry-run must not mutate settings.json")
	}
}

func TestDryRun_ClaudeInstalledDetectedViaSettingsHooks(t *testing.T) {
	// Claude's settings.json is the only install target that cannot be
	// detected by writeFile presence alone (it exists for every claude user).
	// Verify the installClaudeHooks nudge correctly sets installed=true when
	// existing pop hooks are found. settings.json is the sole status-wiring
	// artifact now that skills are off the default path.
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	d := withDryRun(fakeDeps("/h", fs, io.Discard))
	if err := runIntegrateWith(d, "claude"); err != nil {
		t.Fatalf("dry-run claude: %v", err)
	}

	if !d.installed {
		t.Error("expected installed=true detected via pop hooks in settings.json")
	}
}

// ----- ensureIntegrationsForRevisionWith -------------------------------------

// seedState writes a state.json with the given revision into XDG_DATA_HOME.
// The caller must have set XDG_DATA_HOME via t.Setenv before calling.
func seedState(t *testing.T, rev string) {
	t.Helper()
	if err := saveAppState(&appState{BuildRevision: rev}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
}

// readStateRevision returns the build_revision currently in state.json, or
// empty string if the file is missing.
func readStateRevision(t *testing.T) string {
	t.Helper()
	return loadAppState().BuildRevision
}

// fakeFactories returns a pair of (dry, real) integrateDeps constructors
// that share a single fake FS at the given home directory.
func fakeFactories(home string, fs *fakeFS) (dry, real func() *integrateDeps) {
	dry = func() *integrateDeps {
		return withDryRun(fakeDeps(home, fs, io.Discard))
	}
	real = func() *integrateDeps {
		return fakeDeps(home, fs, io.Discard)
	}
	return dry, real
}

func TestEnsureIntegrations_SkipsOnDevBuild(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	dry, real := fakeFactories("/h", fs)

	warnings := ensureIntegrationsForRevisionWith("dev", dry, real)
	if warnings != nil {
		t.Errorf("expected nil warnings for dev build, got %v", warnings)
	}
	if readStateRevision(t) != "" {
		t.Error("dev build must not stamp state.json")
	}
}

func TestEnsureIntegrations_SkipsWhenRevisionMatches(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	seedState(t, "abc123")

	calls := 0
	fs := newFakeFS()
	dry := func() *integrateDeps {
		calls++
		return withDryRun(fakeDeps("/h", fs, io.Discard))
	}
	real := func() *integrateDeps {
		t.Fatal("real deps factory must not be called when revision matches")
		return nil
	}

	warnings := ensureIntegrationsForRevisionWith("abc123", dry, real)
	if warnings != nil {
		t.Errorf("expected nil warnings, got %v", warnings)
	}
	if calls != 0 {
		t.Errorf("expected 0 dry-run calls when revision matches, got %d", calls)
	}
}

func TestEnsureIntegrations_SkipsUninstalledAgents(t *testing.T) {
	// No agents installed: ensureIntegrations should do nothing, return no
	// warnings, and stamp the new revision.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	realCalls := 0
	dry := func() *integrateDeps {
		return withDryRun(fakeDeps("/h", fs, io.Discard))
	}
	real := func() *integrateDeps {
		realCalls++
		return fakeDeps("/h", fs, io.Discard)
	}

	warnings := ensureIntegrationsForRevisionWith("rev1", dry, real)
	if warnings != nil {
		t.Errorf("expected nil warnings for no-install case, got %v", warnings)
	}
	if realCalls != 0 {
		t.Errorf("expected no real install calls, got %d", realCalls)
	}
	if got := readStateRevision(t); got != "rev1" {
		t.Errorf("state.json revision = %q, want %q", got, "rev1")
	}
}

func TestEnsureIntegrations_UpdatesStaleAgent(t *testing.T) {
	// Seed claude as installed-but-stale; pi and opencode uninstalled.
	// ensureIntegrations should run the real install for claude only and
	// stamp state.json.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	// Corrupt the settings.json so claude's status wiring shows as stale:
	// an old-style pop hook marks it installed, but the bytes differ from the
	// current serialization so the dry-run reports changed.
	stalePath := filepath.Join("/h", ".claude", "settings.json")
	if _, exists := fs.files[stalePath]; !exists {
		t.Fatalf("seed install did not produce %s", stalePath)
	}
	fs.files[stalePath] = []byte(staleClaudeSettings)

	dry, real := fakeFactories("/h", fs)

	warnings := ensureIntegrationsForRevisionWith("rev2", dry, real)
	if warnings != nil {
		t.Errorf("expected no warnings on successful update, got %v", warnings)
	}
	// Settings should now carry the current pop hooks and the stale marker
	// should be gone.
	if bytes.Contains(fs.files[stalePath], []byte("pop-status unread")) {
		t.Errorf("stale claude hook was not refreshed")
	}
	if !bytes.Contains(fs.files[stalePath], []byte("pop pane set-status clear")) {
		t.Errorf("claude settings not updated to current hooks")
	}
	if got := readStateRevision(t); got != "rev2" {
		t.Errorf("state.json revision = %q, want %q", got, "rev2")
	}
}

func TestEnsureIntegrations_RetriesOnFailure(t *testing.T) {
	// Seed claude as installed-but-stale, then inject a write error for the
	// real install. ensureIntegrations should return a warning and leave
	// state.json unstamped so the next launch retries.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	stalePath := filepath.Join("/h", ".claude", "settings.json")
	fs.files[stalePath] = []byte(staleClaudeSettings)

	// Inject a write error on the file we're about to update.
	fs.writeErr[stalePath] = errors.New("simulated write failure")

	dry, real := fakeFactories("/h", fs)
	warnings := ensureIntegrationsForRevisionWith("rev3", dry, real)

	if len(warnings) == 0 {
		t.Fatal("expected a warning for claude update failure")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "claude") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a warning mentioning claude, got %v", warnings)
	}
	if got := readStateRevision(t); got != "" {
		t.Errorf("state.json must not be stamped on failure; got revision %q", got)
	}
}

func TestEnsureIntegrations_PartialFailureDoesNotStamp(t *testing.T) {
	// Seed claude AND pi as installed-but-stale. Let claude update succeed
	// but fail pi. state.json must not be stamped.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	installViaFake(t, fs, "/h", "pi")

	// Make claude stale.
	clauseStale := filepath.Join("/h", ".claude", "settings.json")
	fs.files[clauseStale] = []byte(staleClaudeSettings)

	// Make pi stale AND fail its write.
	piExtPath := filepath.Join("/h", ".pi", "agent", "extensions", "pop-status-sync.ts")
	fs.files[piExtPath] = []byte("STALE PI EXT")
	fs.writeErr[piExtPath] = errors.New("pi write failure")

	dry, real := fakeFactories("/h", fs)
	warnings := ensureIntegrationsForRevisionWith("rev4", dry, real)

	// claude should have updated cleanly.
	if !bytes.Contains(fs.files[clauseStale], []byte("pop pane set-status clear")) {
		t.Error("claude stale file should have been updated")
	}
	// pi should have produced a warning.
	if len(warnings) == 0 {
		t.Fatal("expected warning for pi failure")
	}
	foundPi := false
	for _, w := range warnings {
		if strings.Contains(w, "pi") {
			foundPi = true
			break
		}
	}
	if !foundPi {
		t.Errorf("expected warning mentioning pi, got %v", warnings)
	}
	// state.json must NOT be stamped: next launch retries.
	if got := readStateRevision(t); got != "" {
		t.Errorf("state.json must not be stamped on partial failure; got %q", got)
	}
}

// ----- per-component refresh -------------------------------------------------

// seedFileComponent runs a real install of a file-based component against the
// fake FS so refresh tests start from an installed-and-current state.
func seedFileComponent(t *testing.T, fs *fakeFS, home string, id ComponentID, agent string) {
	t.Helper()
	if err := installFileComponent(fakeDeps(home, fs, io.Discard), home, id, agent); err != nil {
		t.Fatalf("seed install %s/%s: %v", agent, id, err)
	}
}

// claudePaneRenderFile is the rendered SKILL.md path for claude's pane skill
// under the fake FS data dir — the file refresh tests corrupt to force stale.
func claudePaneRenderFile(home string) string {
	return filepath.Join(home, ".local", "share", "pop", "integrations", "claude", "pane-skill", "pop-pane", "SKILL.md")
}

func claudePaneLink(home string) string {
	return filepath.Join(home, ".claude", "skills", "pop-pane")
}

func TestEnsureIntegrations_RefreshesStaleFileComponent(t *testing.T) {
	// Seed claude's pane skill as installed, then corrupt the rendered bytes so
	// the component reads as stale. Refresh on a new revision should re-render
	// it, record claude as updated, and stamp state.json.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")

	renderFile := claudePaneRenderFile("/h")
	want := append([]byte{}, fs.files[renderFile]...)
	if len(want) == 0 {
		t.Fatalf("seed did not render %s", renderFile)
	}
	fs.files[renderFile] = []byte("stale skill body")

	dry, real := fakeFactories("/h", fs)
	warnings := ensureIntegrationsForRevisionWith("rev-fc1", dry, real)
	if warnings != nil {
		t.Errorf("expected no warnings on successful file-component refresh, got %v", warnings)
	}
	if !bytes.Equal(fs.files[renderFile], want) {
		t.Errorf("stale pane-skill render not refreshed:\n got %q\nwant %q", fs.files[renderFile], want)
	}
	if got := readStateRevision(t); got != "rev-fc1" {
		t.Errorf("state.json revision = %q, want %q", got, "rev-fc1")
	}
}

func TestEnsureIntegrations_NeverAddsUninstalledFileComponent(t *testing.T) {
	// Nothing installed for any agent. Refresh must add nothing — no render
	// files, no symlinks — and still stamp the revision (a clean no-op pass).
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()

	dry, real := fakeFactories("/h", fs)
	warnings := ensureIntegrationsForRevisionWith("rev-fc2", dry, real)
	if warnings != nil {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if len(fs.symlinks) != 0 {
		t.Errorf("refresh must never add a component: created symlinks %v", fs.symlinks)
	}
	if len(fs.files) != 0 {
		t.Errorf("refresh must never add a component: wrote files %v", sortedKeys(fs.files))
	}
	if got := readStateRevision(t); got != "rev-fc2" {
		t.Errorf("state.json revision = %q, want %q", got, "rev-fc2")
	}
}

func TestEnsureIntegrations_LeavesCurrentFileComponentUntouched(t *testing.T) {
	// An installed-and-current file component must not be touched by refresh:
	// no warnings, and the agent is not reported as updated.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")

	link := claudePaneLink("/h")
	targetBefore := fs.symlinks[link]

	result := updateStaleIntegrations(fakeFactories("/h", fs))
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings for current component, got %v", result.Warnings)
	}
	for _, a := range result.Updated {
		if a == "claude" {
			t.Errorf("claude reported updated though its components are current")
		}
	}
	if fs.symlinks[link] != targetBefore {
		t.Errorf("current component's symlink was rewritten: %q -> %q", targetBefore, fs.symlinks[link])
	}
}

func TestRefreshComponent_SkipsConflictSilently(t *testing.T) {
	// An unowned entry shadowing pop's skill (the bare `pane` name) is an
	// Integration conflict. Refresh must skip it silently — no update, no
	// warning, and no symlink written over the user's entry.
	fs := newFakeFS()
	conflict := filepath.Join("/h", ".claude", "skills", "pane")
	fs.dirs[conflict] = true

	dry, real := fakeFactories("/h", fs)
	updated, warning := refreshComponent(dry, real, "claude", ComponentPaneSkill)
	if updated {
		t.Errorf("expected no update on conflict")
	}
	if warning != "" {
		t.Errorf("conflict must be silent, got warning %q", warning)
	}
	if len(fs.symlinks) != 0 {
		t.Errorf("conflict must not write a symlink, got %v", fs.symlinks)
	}
}

func TestRefreshComponent_SkipsNotSupportedSilently(t *testing.T) {
	// codex hosts neither skill component. Refresh of an unsupported pair must
	// be a silent no-op regardless of on-disk state.
	fs := newFakeFS()
	dry, real := fakeFactories("/h", fs)

	for _, id := range []ComponentID{ComponentPaneSkill, ComponentTaskSkills} {
		updated, warning := refreshComponent(dry, real, "codex", id)
		if updated || warning != "" {
			t.Errorf("codex/%s: expected silent no-op, got updated=%v warning=%q", id, updated, warning)
		}
	}
}

func TestEnsureIntegrations_MigratesCopyModeToSymlink(t *testing.T) {
	// A pre-symlink copy-mode install (a real `pop-pane` directory at the agent
	// location, no render tree under the data dir) is pop-owned but stale.
	// Refresh must migrate it to a symlink into the freshly rendered tree.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()

	link := claudePaneLink("/h")
	fs.dirs[link] = true
	copyFile := filepath.Join(link, "SKILL.md")
	fs.files[copyFile] = []byte("old copy-mode body")

	dry, real := fakeFactories("/h", fs)
	warnings := ensureIntegrationsForRevisionWith("rev-fc3", dry, real)
	if warnings != nil {
		t.Errorf("expected no warnings on migration, got %v", warnings)
	}
	if _, ok := fs.files[copyFile]; ok {
		t.Errorf("copy-mode file not removed during migration: %s", copyFile)
	}
	if fs.dirs[link] {
		t.Errorf("copy-mode directory not removed during migration: %s", link)
	}
	target := filepath.Join("/h", ".local", "share", "pop", "integrations", "claude", "pane-skill", "pop-pane")
	if fs.symlinks[link] != target {
		t.Errorf("expected migration to symlink %q -> %q, got %q", link, target, fs.symlinks[link])
	}
	renderFile := claudePaneRenderFile("/h")
	if _, ok := fs.files[renderFile]; !ok {
		t.Errorf("render tree not written during migration: %s", renderFile)
	}
	if got := readStateRevision(t); got != "rev-fc3" {
		t.Errorf("state.json revision = %q, want %q", got, "rev-fc3")
	}
}

func TestUpdateExisting_RefreshesFileComponentPerAgent(t *testing.T) {
	// The packaging path refreshes file components too, keeping its one-line-
	// per-agent output. Seed claude's pane skill stale; the update-existing run
	// should print exactly one "Updated claude" line and stamp the revision.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")
	fs.files[claudePaneRenderFile("/h")] = []byte("stale skill body")

	dry, real := fakeFactories("/h", fs)
	var stdout, stderr bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-fc4", dry, real, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := stdout.String(), "✓ Updated claude integration\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no warnings, got %q", stderr.String())
	}
	if got := readStateRevision(t); got != "rev-fc4" {
		t.Errorf("state.json revision = %q, want %q", got, "rev-fc4")
	}
}

// ----- appState load/save ----------------------------------------------------

func TestAppState_LoadMissingReturnsEmpty(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := loadAppState()
	if s == nil {
		t.Fatal("loadAppState returned nil for missing file; want empty struct")
	}
	if s.BuildRevision != "" {
		t.Errorf("BuildRevision = %q, want empty", s.BuildRevision)
	}
}

func TestAppState_LoadCorruptReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	popDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(popDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(popDir, "state.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := loadAppState()
	if s.BuildRevision != "" {
		t.Errorf("corrupt state.json should produce empty revision, got %q", s.BuildRevision)
	}
}

func TestAppState_SaveThenLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := saveAppState(&appState{BuildRevision: "deadbeef"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadAppState()
	if got.BuildRevision != "deadbeef" {
		t.Errorf("round-trip revision = %q, want %q", got.BuildRevision, "deadbeef")
	}
}

// ----- runIntegrateUpdateExistingWith (pop integrate --update-existing) -----

func TestUpdateExisting_SilentOnNoInstallations(t *testing.T) {
	// No agents installed anywhere: the command should print nothing and
	// stamp state.json with the current revision (so the runtime fast-path
	// has nothing to do on the next launch either).
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	dry, real := fakeFactories("/h", fs)

	var stdout, stderr bytes.Buffer
	err := runIntegrateUpdateExistingWith("rev1", dry, real, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected silent stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected silent stderr, got %q", stderr.String())
	}
	if got := readStateRevision(t); got != "rev1" {
		t.Errorf("state.json revision = %q, want %q", got, "rev1")
	}
}

func TestUpdateExisting_PrintsLinePerUpdatedAgent(t *testing.T) {
	// Seed claude + pi as installed-but-stale. Both should update; stdout
	// should get one line per updated agent in the declaration order of
	// integrationAgents (claude, codex, pi, opencode). codex and opencode
	// aren't installed and must not appear.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	installViaFake(t, fs, "/h", "pi")

	// Make both stale.
	clauseStale := filepath.Join("/h", ".claude", "settings.json")
	fs.files[clauseStale] = []byte(staleClaudeSettings)
	piStale := filepath.Join("/h", ".pi", "agent", "extensions", "pop-status-sync.ts")
	fs.files[piStale] = []byte("STALE pi")

	dry, real := fakeFactories("/h", fs)

	var stdout, stderr bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev2", dry, real, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "✓ Updated claude integration\n✓ Updated pi integration\n"
	if got := stdout.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no warnings, got %q", stderr.String())
	}
	if got := readStateRevision(t); got != "rev2" {
		t.Errorf("state.json revision = %q, want %q", got, "rev2")
	}
}

func TestUpdateExisting_SilentWhenInstalledAndCurrent(t *testing.T) {
	// Seed claude at the exact embedded content. Dry-run sees installed but
	// not changed → no updates, no warnings, stamp state.json anyway.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	dry, real := fakeFactories("/h", fs)
	var stdout, stderr bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev3", dry, real, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected silent stdout on up-to-date install, got %q", stdout.String())
	}
	if got := readStateRevision(t); got != "rev3" {
		t.Errorf("state.json revision = %q, want %q", got, "rev3")
	}
}

func TestUpdateExisting_WritesWarningToStderrAndDoesNotStamp(t *testing.T) {
	// Seed claude stale, inject a write failure on the target. The command
	// should print the warning to stderr, leave stdout empty (nothing
	// actually updated), and NOT stamp state.json so the next runtime
	// check retries.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	stalePath := filepath.Join("/h", ".claude", "settings.json")
	fs.files[stalePath] = []byte(staleClaudeSettings)
	fs.writeErr[stalePath] = errors.New("simulated failure")

	dry, real := fakeFactories("/h", fs)
	var stdout, stderr bytes.Buffer
	err := runIntegrateUpdateExistingWith("rev4", dry, real, &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected nil error (non-fatal), got %v", err)
	}

	// Nothing successfully updated.
	if stdout.Len() != 0 {
		t.Errorf("expected no success lines, got stdout %q", stdout.String())
	}
	// Warning for claude.
	if !strings.Contains(stderr.String(), "claude") {
		t.Errorf("expected stderr to mention claude, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "⚠") {
		t.Errorf("expected warning prefix in stderr, got %q", stderr.String())
	}
	// state.json must not be stamped on failure.
	if got := readStateRevision(t); got != "" {
		t.Errorf("state.json must not be stamped on failure; got %q", got)
	}
}

func TestUpdateExisting_DevRevisionDoesNotStamp(t *testing.T) {
	// A "dev" revision should never stamp state.json (matches the runtime
	// behavior where dev builds are unreliable as staleness markers).
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	dry, real := fakeFactories("/h", fs)

	var stdout, stderr bytes.Buffer
	if err := runIntegrateUpdateExistingWith("dev", dry, real, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readStateRevision(t); got != "" {
		t.Errorf("dev revision must not stamp state.json, got %q", got)
	}
}

// ----- integrate command argument parsing for --update-existing -------------

func TestIntegrateCmd_UpdateExistingWithAgentArgIsError(t *testing.T) {
	// The Args validator should reject passing an agent together with
	// --update-existing. Temporarily set the package-level flag so the
	// validator sees the same state as a real invocation.
	prev := integrateUpdateExisting
	integrateUpdateExisting = true
	t.Cleanup(func() { integrateUpdateExisting = prev })

	err := integrateCmd.Args(integrateCmd, []string{"claude"})
	if err == nil {
		t.Fatal("expected error when --update-existing is combined with an agent argument")
	}
	if !strings.Contains(err.Error(), "--update-existing") {
		t.Errorf("error should mention --update-existing, got %q", err.Error())
	}
}

func TestIntegrateCmd_UpdateExistingWithNoArgsIsOK(t *testing.T) {
	prev := integrateUpdateExisting
	integrateUpdateExisting = true
	t.Cleanup(func() { integrateUpdateExisting = prev })

	if err := integrateCmd.Args(integrateCmd, []string{}); err != nil {
		t.Errorf("expected no error for --update-existing with no args, got %v", err)
	}
}

func TestIntegrateCmd_WithoutFlagRequiresExactlyOneArg(t *testing.T) {
	prev := integrateUpdateExisting
	integrateUpdateExisting = false
	t.Cleanup(func() { integrateUpdateExisting = prev })

	if err := integrateCmd.Args(integrateCmd, []string{}); err == nil {
		t.Error("expected error when no agent argument is provided")
	}
	if err := integrateCmd.Args(integrateCmd, []string{"claude"}); err != nil {
		t.Errorf("expected no error for single agent arg, got %v", err)
	}
	if err := integrateCmd.Args(integrateCmd, []string{"claude", "pi"}); err == nil {
		t.Error("expected error when more than one agent is provided")
	}
}
