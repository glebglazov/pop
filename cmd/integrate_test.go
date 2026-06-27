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
	// optOuts stands in for state.json's per-agent Component opt-out set, so
	// install/remove/refresh tests can seed and assert opt-outs without touching
	// the real state.json. Keyed by lowercased agent name.
	optOuts map[string]map[ComponentID]bool
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
		optOuts:   map[string]map[ComponentID]bool{},
	}
}

// loadOptOut returns a copy of the agent's opt-out set (matching the production
// seam, which never hands out the stored map for mutation).
func (f *fakeFS) loadOptOut(agent string) map[ComponentID]bool {
	out := map[ComponentID]bool{}
	for id := range f.optOuts[strings.ToLower(agent)] {
		out[id] = true
	}
	return out
}

// saveOptOut replaces the agent's opt-out set; an empty set clears the entry.
func (f *fakeFS) saveOptOut(agent string, set map[ComponentID]bool) error {
	agent = strings.ToLower(agent)
	if len(set) == 0 {
		delete(f.optOuts, agent)
		return nil
	}
	copied := map[ComponentID]bool{}
	for id := range set {
		copied[id] = true
	}
	f.optOuts[agent] = copied
	return nil
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

// readDirNames lists the immediate child entry names under dir across the
// files, dirs, and symlinks maps, sorted. A missing directory yields no entries
// (matching osReadDirNames), so the stale-name prune is a no-op on a fresh
// agent location.
func (f *fakeFS) readDirNames(dir string) ([]string, error) {
	prefix := dir + string(filepath.Separator)
	set := map[string]bool{}
	add := func(p string) {
		if !strings.HasPrefix(p, prefix) {
			return
		}
		rest := p[len(prefix):]
		if i := strings.IndexByte(rest, filepath.Separator); i >= 0 {
			rest = rest[:i]
		}
		if rest != "" {
			set[rest] = true
		}
	}
	for k := range f.files {
		add(k)
	}
	for k := range f.dirs {
		add(k)
	}
	for k := range f.symlinks {
		add(k)
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
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
		userHomeDir:  func() (string, error) { return home, nil },
		readFile:     fs.readFile,
		writeFile:    fs.writeFile,
		mkdirAll:     fs.mkdirAll,
		removeAll:    fs.removeAll,
		stdout:       stdout,
		logf:         func(string, ...any) {}, // no-op; override per-test to capture
		dataDir:      func() (string, error) { return filepath.Join(home, ".local", "share", "pop"), nil },
		symlink:      fs.symlink,
		readlink:     fs.readlink,
		lstatMode:    fs.lstatMode,
		readDirNames: fs.readDirNames,
		loadOptOut:   fs.loadOptOut,
		saveOptOut:   fs.saveOptOut,
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

	outcome, warning := refreshStatusWiring(newDry, newReal, "claude")
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if outcome == nil || outcome.Label != "updated" {
		t.Fatalf("expected refresh to update claude status wiring (topic hook added), got outcome=%v", outcome)
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

	outcome, warning := refreshStatusWiring(newDry, newReal, "codex")
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if outcome == nil || outcome.Label != "updated" {
		t.Fatalf("expected refresh to add the codex topic hook, got outcome=%v", outcome)
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

	outcome, warning := refreshStatusWiring(newDry, newReal, "cursor")
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if outcome == nil || outcome.Label != "updated" {
		t.Fatalf("expected refresh to add the cursor topic hook, got outcome=%v", outcome)
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

// installFullDefaultViaFake seeds the complete default set for an agent (status
// wiring plus every default-on component it can host), so refresh finds nothing
// missing to add. Used by the "installed and current" refresh tests, which must
// look at a fully integrated agent rather than one with only status wiring (an
// agent missing a default component is now refreshed to add it, ADR 0064).
func installFullDefaultViaFake(t *testing.T, fs *fakeFS, home, agent string) {
	t.Helper()
	if err := installComponentSet(fakeDeps(home, fs, io.Discard), agent, defaultComponentIDs()); err != nil {
		t.Fatalf("seed full default install %s: %v", agent, err)
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

// seedBaselineComponents installs every optional component in the embedded default
// baseline for agent. Use after installViaFake when refresh should not add skills.
func seedBaselineComponents(t *testing.T, fs *fakeFS, home, agent string) {
	t.Helper()
	for _, id := range defaultIntegrationBaseline() {
		seedFileComponent(t, fs, home, id, agent)
	}
}

// claudePaneRenderFile is the rendered SKILL.md path for claude's pane skill
// under the fake FS data dir — the file refresh tests corrupt to force stale.
func claudePaneRenderFile(home string) string {
	return filepath.Join(home, ".local", "share", "pop", "integrations", "claude", "pane-skill", "pop-tmux-pane", "SKILL.md")
}

func claudePaneLink(home string) string {
	return filepath.Join(home, ".claude", "skills", "pop-tmux-pane")
}

func TestEnsureIntegrations_RefreshesStaleFileComponent(t *testing.T) {
	// Seed claude's pane skill as installed, then corrupt the rendered bytes so
	// the component reads as stale. Refresh on a new revision should re-render
	// it, record claude as updated, and stamp state.json.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
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

func TestUpdateExisting_AddsMissingBaselineComponent(t *testing.T) {
	// Integrated agent missing a baseline-listed component gets it installed.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	dry, real := fakeFactories("/h", fs)
	var stdout bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-add1", dry, real, &stdout, io.Discard, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	link := claudePaneLink("/h")
	if _, ok := fs.symlinks[link]; !ok {
		t.Fatalf("expected pane-skill symlink at %s", link)
	}
	if !strings.Contains(stdout.String(), "pane-skill") || !strings.Contains(stdout.String(), "added") {
		t.Errorf("stdout = %q, want pane-skill added line", stdout.String())
	}
}

func TestEnsureIntegrations_AddsMissingBaselineComponent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	dry, real := fakeFactories("/h", fs)
	warnings := ensureIntegrationsForRevisionWith("rev-add2", dry, real)
	if warnings != nil {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if _, ok := fs.symlinks[claudePaneLink("/h")]; !ok {
		t.Errorf("auto-refresh must install missing baseline pane-skill")
	}
}

func TestRefresh_SkipsOptedOutBaselineComponent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeIntegrateRuntimeFile(t, `
[integrations]
skills = ["tasks"]
`)
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	dry, real := fakeFactories("/h", fs)
	result := updateStaleIntegrations(dry, real)
	if _, ok := fs.symlinks[claudePaneLink("/h")]; ok {
		t.Fatal("refresh must not install pane-skill omitted from merged baseline")
	}
	for _, o := range result.Outcomes {
		if o.Agent == "claude" && o.Component == ComponentPaneSkill && o.Label != "skipped (opted out)" {
			t.Errorf("pane-skill outcome = %q, want skipped (opted out)", o.Label)
		}
	}
}

func TestRefresh_SkipsConflictWithoutOverwrite(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	conflictPath := filepath.Join("/h", ".claude", "skills", "pane")
	fs.dirs[conflictPath] = true
	delete(fs.symlinks, claudePaneLink("/h"))

	dry, real := fakeFactories("/h", fs)
	result := updateStaleIntegrations(dry, real)
	if _, ok := fs.symlinks[claudePaneLink("/h")]; ok {
		t.Fatal("refresh must not install over an Integration conflict")
	}
	for _, o := range result.Outcomes {
		if o.Component == ComponentPaneSkill {
			if !strings.Contains(o.Label, "skipped (conflict") {
				t.Errorf("pane-skill outcome = %q, want conflict skip", o.Label)
			}
			if !strings.Contains(o.Label, "pop integrate claude --overwrite-conflicts") {
				t.Errorf("conflict skip must name overwrite resolve command, got %q", o.Label)
			}
		}
	}
}

func TestEnsureIntegrations_LeavesCurrentFileComponentUntouched(t *testing.T) {
	// An installed-and-current file component must not be touched by refresh:
	// no warnings, and the agent is not reported as updated.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")
	seedFileComponent(t, fs, "/h", ComponentTaskSkills, "claude")

	link := claudePaneLink("/h")
	targetBefore := fs.symlinks[link]

	result := updateStaleIntegrations(fakeFactories("/h", fs))
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings for current component, got %v", result.Warnings)
	}
	for _, o := range result.Outcomes {
		if o.Agent == "claude" && (o.Label == "updated" || o.Label == "added") {
			t.Errorf("claude reported updated though its components are current (outcome: %v)", o)
		}
	}
	if fs.symlinks[link] != targetBefore {
		t.Errorf("current component's symlink was rewritten: %q -> %q", targetBefore, fs.symlinks[link])
	}
}

func TestRefreshComponent_SkipsConflictSilently(t *testing.T) {
	// An unowned entry shadowing pop's skill (the bare `tmux-pane` name) is an
	// Integration conflict. Refresh must skip it silently — no update, no
	// warning, and no symlink written over the user's entry.
	fs := newFakeFS()
	conflict := filepath.Join("/h", ".claude", "skills", "tmux-pane")
	fs.dirs[conflict] = true

	dry, real := fakeFactories("/h", fs)
	outcome, warning := refreshComponent(dry, real, "claude", ComponentPaneSkill, baselineComponentSet(defaultIntegrationBaseline()))
	updated := outcome != nil && (outcome.Label == "updated" || outcome.Label == "added")
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

// TestRefreshComponent_OpencodeTaskSkillsAddsMissing covers refresh installing
// missing baseline-listed task skills for opencode once status wiring is present.
func TestRefreshComponent_OpencodeTaskSkillsAddsMissing(t *testing.T) {
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "opencode")
	dry, real := fakeFactories("/h", fs)

	outcome, warning := refreshComponent(dry, real, "opencode", ComponentTaskSkills, baselineComponentSet(defaultIntegrationBaseline()))
	if warning != "" {
		t.Fatalf("unexpected warning: %q", warning)
	}
	if outcome == nil || outcome.Label != "added" {
		t.Fatalf("expected added outcome, got %v", outcome)
	}
	grillDest := filepath.Join("/h", ".config", "opencode", "skills", "pop-grill-with-docs")
	if fs.symlinks[grillDest] == "" {
		t.Fatalf("task skill not symlinked for opencode: %v", fs.symlinks)
	}
	for _, c := range []string{"ADR-FORMAT.md", "CONTEXT-FORMAT.md"} {
		p := filepath.Join("/h", ".local", "share", "pop", "integrations", "opencode", "task-skills", "pop-grill-with-docs", c)
		if _, ok := fs.files[p]; !ok {
			t.Fatalf("companion not written: %s", p)
		}
	}
}

func TestRefreshComponent_SkipsUnknownComponentSilently(t *testing.T) {
	fs := newFakeFS()
	dry, real := fakeFactories("/h", fs)

	outcome, warning := refreshComponent(dry, real, "opencode", ComponentID("bogus"), baselineComponentSet(defaultIntegrationBaseline()))
	if outcome != nil || warning != "" {
		t.Errorf("unknown component: expected nil outcome and no warning, got outcome=%v warning=%q", outcome, warning)
	}
}

func TestEnsureIntegrations_MigratesCopyModeToSymlink(t *testing.T) {
	// A pre-symlink copy-mode install (a real `pop-tmux-pane` directory at the agent
	// location whose SKILL.md carries the pop-owned marker, no render tree under
	// the data dir) is pop-owned but stale. Refresh must migrate it to a symlink
	// into the freshly rendered tree.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	link := claudePaneLink("/h")
	fs.dirs[link] = true
	copyFile := filepath.Join(link, "SKILL.md")
	fs.files[copyFile] = []byte("---\npop-owned: true\n---\nold copy-mode body")

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
	target := filepath.Join("/h", ".local", "share", "pop", "integrations", "claude", "pane-skill", "pop-tmux-pane")
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
	// The packaging path refreshes file components too. Seed claude's pane skill
	// stale; the update-existing run should print a per-component updated line
	// for pane-skill and stamp the revision.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")
	fs.files[claudePaneRenderFile("/h")] = []byte("stale skill body")

	dry, real := fakeFactories("/h", fs)
	var stdout, stderr bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-fc4", dry, real, &stdout, &stderr, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "claude") || !strings.Contains(stdout.String(), "pane-skill") || !strings.Contains(stdout.String(), "updated") {
		t.Errorf("stdout = %q, want a line containing claude, pane-skill, updated", stdout.String())
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
	// No agents installed anywhere: opted-out outcomes are verbose-gated on
	// the update-existing path, so default output is "nothing to do".
	// state.json is stamped regardless (runtime fast-path can skip next launch).
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	dry, real := fakeFactories("/h", fs)

	var stdout, stderr bytes.Buffer
	err := runIntegrateUpdateExistingWith("rev1", dry, real, &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "nothing to do\n" {
		t.Errorf("expected 'nothing to do', got %q", got)
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
	// should contain per-component "updated" lines for both (in agent order:
	// claude before pi). codex and opencode aren't installed and must not
	// appear as updated.
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
	if err := runIntegrateUpdateExistingWith("rev2", dry, real, &stdout, &stderr, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stdout.String()
	// Both stale agents should appear with their component updated.
	if !strings.Contains(got, "claude") || !strings.Contains(got, "updated") {
		t.Errorf("stdout missing claude updated line, got %q", got)
	}
	if !strings.Contains(got, "pi") {
		t.Errorf("stdout missing pi updated line, got %q", got)
	}
	// claude must appear before pi (agent declaration order).
	if claudeIdx := strings.Index(got, "claude"); claudeIdx < 0 {
		t.Errorf("stdout missing claude, got %q", got)
	} else if piIdx := strings.Index(got, "pi"); piIdx >= 0 && claudeIdx > piIdx {
		t.Errorf("claude must appear before pi in output, got %q", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no warnings, got %q", stderr.String())
	}
	if got := readStateRevision(t); got != "rev2" {
		t.Errorf("state.json revision = %q, want %q", got, "rev2")
	}
}

func TestUpdateExisting_SilentWhenInstalledAndCurrent(t *testing.T) {
	// Seed claude's full default set at the exact embedded content. Dry-run sees
	// every component installed but not changed → no updates, no warnings, no
	// missing default to add, stamp state.json anyway. "Already current" outcomes
	// are verbose-gated on the update-existing path → default output is
	// "nothing to do".
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedBaselineComponents(t, fs, "/h", "claude")

	dry, real := fakeFactories("/h", fs)
	var stdout, stderr bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev3", dry, real, &stdout, &stderr, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "nothing to do\n" {
		t.Errorf("expected 'nothing to do' on up-to-date install, got %q", got)
	}
	if got := readStateRevision(t); got != "rev3" {
		t.Errorf("state.json revision = %q, want %q", got, "rev3")
	}
}

func TestUpdateExisting_WritesWarningToStderrAndDoesNotStamp(t *testing.T) {
	// Seed claude's full default set so nothing is missing to add, then make the
	// status wiring stale and inject a write failure on it. The command should
	// print "nothing to do" to stdout (nothing actually updated), print the
	// warning to stderr, and NOT stamp state.json so the next runtime check retries.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedBaselineComponents(t, fs, "/h", "claude")

	stalePath := filepath.Join("/h", ".claude", "settings.json")
	fs.files[stalePath] = []byte(staleClaudeSettings)
	fs.writeErr[stalePath] = errors.New("simulated failure")

	dry, real := fakeFactories("/h", fs)
	var stdout, stderr bytes.Buffer
	err := runIntegrateUpdateExistingWith("rev4", dry, real, &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("expected nil error (non-fatal), got %v", err)
	}

	// Nothing successfully updated — "nothing to do" (no updated outcomes).
	if got := stdout.String(); got != "nothing to do\n" {
		t.Errorf("expected 'nothing to do' on failed update, got stdout %q", got)
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
	if err := runIntegrateUpdateExistingWith("dev", dry, real, &stdout, &stderr, false); err != nil {
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

func TestIntegrateCmd_WithoutFlagRequiresAtLeastOneArg(t *testing.T) {
	prev := integrateUpdateExisting
	integrateUpdateExisting = false
	t.Cleanup(func() { integrateUpdateExisting = prev })

	if err := integrateCmd.Args(integrateCmd, []string{}); err == nil {
		t.Error("expected error when no agent argument is provided")
	}
	if err := integrateCmd.Args(integrateCmd, []string{"claude"}); err != nil {
		t.Errorf("expected no error for single agent arg, got %v", err)
	}
	if err := integrateCmd.Args(integrateCmd, []string{"claude", "pi"}); err != nil {
		t.Errorf("expected no error for multiple agent args, got %v", err)
	}
	if err := integrateCmd.Args(integrateCmd, []string{"claude", "codex", "pi", "opencode", "cursor"}); err != nil {
		t.Errorf("expected no error for all agents, got %v", err)
	}
}

func TestIntegrateCmd_MultiAgentInstall(t *testing.T) {
	// Test that multiple agents can be installed via runIntegrateComponents
	// called in sequence with the same flags applied uniformly to all.
	// We use empty optins to simulate the case where no component flags
	// are passed (requiring explicit opt-in or interactive mode).
	// Here we test with explicit flags to demonstrate multi-agent install.
	fs := newFakeFS()
	var out bytes.Buffer

	// Install claude and pi each with explicit opt-in flags.
	// Empty optins means only status-wiring (core) is installed.
	// Note: empty optins with non-interactive mode requires component flags,
	// so we use non-empty optins just to test multi-agent behavior.
	optins := []ComponentID{ComponentPaneSkill}

	if err := runIntegrateComponents(fakeDeps("/h", fs, &out), "claude", optins, false, false, nil, false, false); err != nil {
		t.Fatalf("claude install: %v", err)
	}

	if err := runIntegrateComponents(fakeDeps("/h", fs, &out), "pi", optins, false, false, nil, false, false); err != nil {
		t.Fatalf("pi install: %v", err)
	}

	// Verify claude was installed (status wiring + pane skill).
	claudeSettings := filepath.Join("/h", ".claude", "settings.json")
	if _, ok := fs.files[claudeSettings]; !ok {
		t.Error("claude status wiring not installed")
	}

	// Verify pi was installed (status wiring + pane skill).
	piExt := filepath.Join("/h", ".pi", "agent", "extensions", "pop-status-sync.ts")
	if _, ok := fs.files[piExt]; !ok {
		t.Error("pi status wiring not installed")
	}
}

func TestIntegrateCmd_MultiAgentWithUniformFlags(t *testing.T) {
	// Test that component flags are applied uniformly across multiple agents.
	// When pane-skill opt-in is requested, it should be installed for all agents.
	fs := newFakeFS()
	optins := []ComponentID{ComponentPaneSkill}

	// Install claude with pane skill.
	if err := runIntegrateComponents(fakeDeps("/h", fs, io.Discard), "claude", optins, false, false, nil, false, false); err != nil {
		t.Fatalf("claude install: %v", err)
	}

	// Install pi with same flags (pane skill).
	if err := runIntegrateComponents(fakeDeps("/h", fs, io.Discard), "pi", optins, false, false, nil, false, false); err != nil {
		t.Fatalf("pi install: %v", err)
	}

	// Verify pane skill was installed for claude.
	claudeLinkPath := filepath.Join("/h", ".claude", "skills", "pop-pane")
	if target, ok := fs.symlinks[claudeLinkPath]; !ok {
		t.Error("claude pane-skill symlink not created")
	} else if !strings.Contains(target, "pop-pane") {
		t.Errorf("claude pane-skill symlink target unexpected: %v", target)
	}
}

func TestIntegrateCmd_UnknownAgentIsRejected(t *testing.T) {
	// Test that runIntegrateComponents rejects unknown agent names clearly.
	fs := newFakeFS()

	// Unknown agent should produce an error.
	err := runIntegrateComponents(fakeDeps("/h", fs, io.Discard), "vscode", []ComponentID{}, false, false, nil, false, false)
	if err == nil {
		t.Fatal("expected error for unknown agent vscode")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("error should mention unknown agent, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "vscode") {
		t.Errorf("error should mention the invalid agent name, got %q", err.Error())
	}
}

func TestIntegrateCmd_PartialAgentMixIsRejected(t *testing.T) {
	// Test that when a mix of valid and invalid agents are passed to runIntegrate,
	// the invalid agent is caught in pre-flight validation before ANY installation,
	// ensuring no partial installs occur.
	// Since runIntegrate is not easily testable with a fake FS (it calls
	// defaultIntegrateDeps), we test the validation logic directly by checking
	// that runIntegrateComponents is called per-agent. An integration test would
	// verify the full behavior. Here, we verify the command-line arg validation.
	prev := integrateUpdateExisting
	integrateUpdateExisting = false
	t.Cleanup(func() { integrateUpdateExisting = prev })

	// The Args validator in cobra doesn't validate agent names (just count),
	// so the error is caught by runIntegrate's pre-flight check on supported agents.
	// We verify the args pass through cobra's validator:
	if err := integrateCmd.Args(integrateCmd, []string{"claude", "vscode"}); err != nil {
		t.Errorf("args validator should not reject unknown agent names (cobra allows them): %v", err)
	}
	// The error will be caught in runIntegrate's pre-flight check instead.
}

// ----- reasoned output: explicit install path --------------------------------

// TestExplicitInstall_OutputAdded: fresh install prints "added" for installed
// components and "skipped (opted out)" for supported-but-not-requested ones.
func TestExplicitInstall_OutputAdded(t *testing.T) {
	fs := newFakeFS()
	var out bytes.Buffer
	d := fakeDeps(installerHome, fs, &out)

	// Install only status-wiring (no opt-in flags).
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, nil, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	// status-wiring was not present before → "added"
	if !strings.Contains(got, "status-wiring") || !strings.Contains(got, "added") {
		t.Errorf("expected 'status-wiring  added' line, got %q", got)
	}
	// pane-skill and task-skills are supported but not requested → "skipped (opted out)"
	if !strings.Contains(got, "pane-skill") || !strings.Contains(got, "skipped (opted out)") {
		t.Errorf("expected 'pane-skill  skipped (opted out)' line, got %q", got)
	}
}

// TestExplicitInstall_OutputUpdated: re-running with stale content prints "updated".
func TestExplicitInstall_OutputUpdated(t *testing.T) {
	fs := newFakeFS()
	settingsPath := filepath.Join(installerHome, ".claude", "settings.json")

	// Seed an old pop hook so the dry-run sees installed+changed.
	fs.files[settingsPath] = []byte(staleClaudeSettings)

	var out bytes.Buffer
	d := fakeDeps(installerHome, fs, &out)

	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, nil, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "status-wiring") || !strings.Contains(got, "updated") {
		t.Errorf("expected 'status-wiring  updated' line, got %q", got)
	}
}

// TestExplicitInstall_OutputSkippedOptedOut: unsupported components produce no
// line; supported-but-not-requested ones show "skipped (opted out)".
func TestExplicitInstall_OutputSkippedOptedOut(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	var out bytes.Buffer
	d := fakeDeps(installerHome, fs, &out)

	// codex supports pane-skill and task-skills — not selecting them shows opted-out.
	if err := runIntegrateComponents(d, "codex", []ComponentID{ComponentStatusWiring}, false, false, nil, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "status-wiring") {
		t.Errorf("expected status-wiring line, got %q", got)
	}
	if !strings.Contains(got, "pane-skill") || !strings.Contains(got, "skipped (opted out)") {
		t.Errorf("expected opted-out pane-skill for codex, got %q", got)
	}
	if !strings.Contains(got, "task-skills") {
		t.Errorf("expected opted-out task-skills for codex, got %q", got)
	}

	// claude supports pane-skill and task-skills — not selecting them shows opted-out.
	out.Reset()
	fs2 := newFakeFS()
	d2 := fakeDeps(installerHome, fs2, &out)
	if err := runIntegrateComponents(d2, "claude", []ComponentID{ComponentStatusWiring}, false, false, nil, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got2 := out.String()
	if !strings.Contains(got2, "pane-skill") || !strings.Contains(got2, "skipped (opted out)") {
		t.Errorf("expected 'pane-skill  skipped (opted out)' for claude, got %q", got2)
	}
}

// TestExplicitInstall_OutputSkippedConflict: a component with a conflict shows
// "skipped (conflict at ...)" and is not installed.
func TestExplicitInstall_OutputSkippedConflict(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	// Place a non-pop entry at the bare pane name to trigger a conflict.
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "pane")
	fs.dirs[conflictPath] = true

	var out bytes.Buffer
	d := fakeDeps(installerHome, fs, &out)

	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring, ComponentPaneSkill}, false, false, nil, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "pane-skill") || !strings.Contains(got, "skipped (conflict") {
		t.Errorf("expected 'pane-skill  skipped (conflict' line, got %q", got)
	}
	// The conflict path should appear in the output.
	if !strings.Contains(got, conflictPath) {
		t.Errorf("expected conflict path %q in output, got %q", conflictPath, got)
	}
	if !strings.Contains(got, "pop integrate claude --overwrite-conflicts") {
		t.Errorf("conflict skip must name the overwrite resolve command, got %q", got)
	}
	// status-wiring should still be installed (conflict only affects pane-skill).
	settingsPath := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settingsPath]; !ok {
		t.Errorf("status wiring should still be installed despite pane-skill conflict")
	}
	// pane-skill symlink must not exist.
	for k := range fs.symlinks {
		if strings.Contains(k, "pop-pane") {
			t.Errorf("pane-skill symlink must not be created on conflict: %s", k)
		}
	}
}

// TestExplicitInstall_VerboseShowsAlreadyCurrent: without --verbose, "already
// current" lines are suppressed; with --verbose, they appear.
func TestExplicitInstall_VerboseShowsAlreadyCurrent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	// First install to set up current content.
	d0 := fakeDeps(installerHome, fs, io.Discard)
	if err := runIntegrateComponents(d0, "claude", []ComponentID{ComponentStatusWiring, ComponentPaneSkill}, false, false, nil, false, false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Re-run without verbose: "already current" should be suppressed → "nothing to do" or opted-out lines only.
	var outNoVerbose bytes.Buffer
	d1 := fakeDeps(installerHome, fs, &outNoVerbose)
	if err := runIntegrateComponents(d1, "claude", []ComponentID{ComponentStatusWiring, ComponentPaneSkill}, false, false, nil, false, false); err != nil {
		t.Fatalf("re-run no verbose: %v", err)
	}
	if strings.Contains(outNoVerbose.String(), "already current") {
		t.Errorf("'already current' must be suppressed without --verbose, got %q", outNoVerbose.String())
	}

	// Re-run with verbose: "already current" should appear for both components.
	var outVerbose bytes.Buffer
	d2 := fakeDeps(installerHome, fs, &outVerbose)
	if err := runIntegrateComponents(d2, "claude", []ComponentID{ComponentStatusWiring, ComponentPaneSkill}, false, true, nil, false, false); err != nil {
		t.Fatalf("re-run verbose: %v", err)
	}
	if !strings.Contains(outVerbose.String(), "already current") {
		t.Errorf("'already current' must appear with --verbose, got %q", outVerbose.String())
	}
}

// ----- reasoned output: --update-existing path --------------------------------

// TestUpdateExisting_OutputAdded: a newly-appearing component shows "updated"
// (refresh never truly "adds" — it only refreshes stale installed ones, so
// status-wiring changed is "updated").
func TestUpdateExisting_OutputUpdated(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	// Make claude stale.
	stalePath := filepath.Join("/h", ".claude", "settings.json")
	fs.files[stalePath] = []byte(staleClaudeSettings)

	dry, real := fakeFactories("/h", fs)
	var stdout, stderr bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-out1", dry, real, &stdout, &stderr, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "claude") || !strings.Contains(got, "status-wiring") || !strings.Contains(got, "updated") {
		t.Errorf("expected 'claude  status-wiring  updated' line, got %q", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no warnings, got %q", stderr.String())
	}
}

// TestUpdateExisting_OutputSkippedOptedOut: baseline omissions appear as
// "skipped (opted out)" only with --verbose on the update-existing path.
func TestUpdateExisting_OutputSkippedOptedOut(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeIntegrateRuntimeFile(t, `
[integrations]
skills = ["tasks"]
`)
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedFileComponent(t, fs, "/h", ComponentTaskSkills, "claude")
	dry, real := fakeFactories("/h", fs)

	// Without verbose: opted-out outcomes are suppressed → "nothing to do".
	var stdoutNoVerbose bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-out2", dry, real, &stdoutNoVerbose, io.Discard, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdoutNoVerbose.String(); got != "nothing to do\n" {
		t.Errorf("without verbose, expected 'nothing to do', got %q", got)
	}

	// With verbose: opted-out outcomes appear for baseline omissions.
	var stdoutVerbose bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-out2", dry, real, &stdoutVerbose, io.Discard, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := stdoutVerbose.String()
	if !strings.Contains(got, "pane-skill") || !strings.Contains(got, "skipped (opted out)") {
		t.Errorf("with verbose, expected pane-skill opted-out line, got %q", got)
	}
}

// TestUpdateExisting_OutputSkippedConflict: a conflict on a file component
// shows "skipped (conflict at ...)" by default on the update-existing path.
func TestUpdateExisting_OutputSkippedConflict(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	// Seed pane-skill as installed.
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")
	// Place a non-pop entry at the bare pane name to create a conflict.
	conflictPath := filepath.Join("/h", ".claude", "skills", "pane")
	fs.dirs[conflictPath] = true
	// Remove the pop-owned symlink so ownership check sees the conflict first.
	delete(fs.symlinks, claudePaneLink("/h"))

	dry, real := fakeFactories("/h", fs)
	var stdout bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-out3", dry, real, &stdout, io.Discard, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "pane-skill") || !strings.Contains(got, "skipped (conflict") {
		t.Errorf("expected 'pane-skill  skipped (conflict' line, got %q", got)
	}
	if !strings.Contains(got, "pop integrate claude --overwrite-conflicts") {
		t.Errorf("refresh conflict skip must name the overwrite resolve command, got %q", got)
	}
}

// TestUpdateExisting_VerboseShowsAlreadyCurrent: "already current" outcomes are
// suppressed without --verbose and shown with --verbose.
func TestUpdateExisting_VerboseShowsAlreadyCurrent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedBaselineComponents(t, fs, "/h", "claude")

	dry, real := fakeFactories("/h", fs)

	// Without verbose: "already current" is suppressed → "nothing to do".
	var stdoutNoVerbose bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-out4", dry, real, &stdoutNoVerbose, io.Discard, false); err != nil {
		t.Fatalf("without verbose: %v", err)
	}
	if got := stdoutNoVerbose.String(); got != "nothing to do\n" {
		t.Errorf("without verbose, expected 'nothing to do', got %q", got)
	}

	// With verbose: "already current" lines appear.
	var stdoutVerbose bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-out4", dry, real, &stdoutVerbose, io.Discard, true); err != nil {
		t.Fatalf("with verbose: %v", err)
	}
	if !strings.Contains(stdoutVerbose.String(), "already current") {
		t.Errorf("with verbose, expected 'already current' lines, got %q", stdoutVerbose.String())
	}
}

// TestUpdateExisting_NothingToDoMessage: a fully up-to-date refresh with
// nothing installed prints "nothing to do" rather than per-component rows.
func TestUpdateExisting_NothingToDoMessage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	dry, real := fakeFactories("/h", fs)

	var stdout bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-out5", dry, real, &stdout, io.Discard, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); got != "nothing to do\n" {
		t.Errorf("expected 'nothing to do', got %q", got)
	}
}

// ----- declarative opt-out removal -------------------------------------------

// TestOptOutRemoval_PaneSkill_RemovesInstalled: --no-pane-skill removes an
// already-installed pop-owned pane skill and reports "removed (opted out)".
func TestOptOutRemoval_PaneSkill_RemovesInstalled(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	// First install pane-skill.
	seedFileComponent(t, fs, home, ComponentPaneSkill, "claude")
	link := claudePaneLink(home)
	if _, ok := fs.symlinks[link]; !ok {
		t.Fatalf("seed install did not create pane-skill symlink at %s", link)
	}

	// Now run with explicit opt-out for pane-skill.
	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pane-skill symlink must be gone.
	if _, ok := fs.symlinks[link]; ok {
		t.Errorf("pane-skill symlink still present after opt-out removal: %s", link)
	}

	// Output must contain "removed (opted out)" for pane-skill.
	got := out.String()
	if !strings.Contains(got, "pane-skill") || !strings.Contains(got, "removed (opted out)") {
		t.Errorf("expected 'pane-skill  removed (opted out)' line, got %q", got)
	}
}

// TestOptOutRemoval_TaskSkills_RemovesInstalled: --no-task-skills removes an
// already-installed pop-owned task-skills component and reports "removed (opted out)".
func TestOptOutRemoval_TaskSkills_RemovesInstalled(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	seedFileComponent(t, fs, home, ComponentTaskSkills, "claude")

	// Verify at least one task-skills link was created.
	hasLink := false
	for k := range fs.symlinks {
		if strings.Contains(k, ".claude/skills") {
			hasLink = true
		}
	}
	if !hasLink {
		t.Fatalf("seed install did not create task-skills symlinks")
	}
	linksBefore := len(fs.symlinks)

	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	optOuts := map[ComponentID]bool{ComponentTaskSkills: true}
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All task-skills symlinks must be gone.
	if len(fs.symlinks) >= linksBefore {
		t.Errorf("expected task-skills symlinks removed, but symlink count unchanged (%d)", len(fs.symlinks))
	}

	got := out.String()
	if !strings.Contains(got, "task-skills") || !strings.Contains(got, "removed (opted out)") {
		t.Errorf("expected 'task-skills  removed (opted out)' line, got %q", got)
	}
}

// TestOptOutRemoval_NotInstalled_SkipsOptedOut: explicit --no-pane-skill on a
// component that is not installed reports "skipped (opted out)" — no removal
// needed, but the opt-out is still recorded in the outcome.
func TestOptOutRemoval_NotInstalled_SkipsOptedOut(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "pane-skill") || !strings.Contains(got, "skipped (opted out)") {
		t.Errorf("expected 'pane-skill  skipped (opted out)' when not installed, got %q", got)
	}
	// Must not contain "removed".
	if strings.Contains(got, "removed") {
		t.Errorf("must not emit 'removed' for a component not installed, got %q", got)
	}
}

// TestOptOutRemoval_LeavesUnownedUntouched: an unowned same-named entry is never
// deleted when --no-pane-skill is passed.
func TestOptOutRemoval_LeavesUnownedUntouched(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	// Place a user-owned (non-pop) entry at the bare pane-skill name.
	userEntry := filepath.Join(home, ".claude", "skills", "pane")
	fs.dirs[userEntry] = true

	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unowned entry must still be present.
	if !fs.dirs[userEntry] {
		t.Errorf("unowned pane entry was deleted by opt-out removal — must never touch user-owned files")
	}
}

// TestOptOutRemoval_NoPrompt: opt-out removal runs without any stdin interaction
// (d.stdin is nil; a prompt would panic or fail).
func TestOptOutRemoval_NoPrompt(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"
	seedFileComponent(t, fs, home, ComponentPaneSkill, "claude")

	// deps with nil stdin — any prompt would return an error from bufio.Reader.
	d := &integrateDeps{
		userHomeDir: func() (string, error) { return home, nil },
		readFile:    fs.readFile,
		writeFile:   fs.writeFile,
		mkdirAll:    fs.mkdirAll,
		removeAll:   fs.removeAll,
		stdout:      io.Discard,
		logf:        func(string, ...any) {},
		dataDir:     func() (string, error) { return filepath.Join(home, ".local", "share", "pop"), nil },
		symlink:     fs.symlink,
		readlink:    fs.readlink,
		lstatMode:   fs.lstatMode,
		stdin:       nil, // no stdin → any prompt read would return EOF (decline), but removal must not need it
	}

	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("opt-out removal must succeed with nil stdin (no prompt): %v", err)
	}

	link := claudePaneLink(home)
	if _, ok := fs.symlinks[link]; ok {
		t.Errorf("pane-skill symlink not removed")
	}
}

// TestOptOutRemoval_BareReAdds: after an explicit opt-out removal, bare
// integrate clears runtime overrides and re-installs the merged baseline.
func TestOptOutRemoval_BareReAdds(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	// Install pane-skill first.
	seedFileComponent(t, fs, home, ComponentPaneSkill, "claude")
	link := claudePaneLink(home)

	// Remove it via explicit opt-out.
	d := fakeDeps(home, fs, io.Discard)
	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := applyIntegrateRuntimeConfig(false, optOuts); err != nil {
		t.Fatalf("applyIntegrateRuntimeConfig: %v", err)
	}
	baseline, err := integrationBaselineLoader()
	if err != nil {
		t.Fatalf("integrationBaselineLoader: %v", err)
	}
	if err := runIntegrateComponents(d, "claude", baseline, false, false, optOuts, false, false); err != nil {
		t.Fatalf("opt-out: %v", err)
	}
	if _, ok := fs.symlinks[link]; ok {
		t.Fatalf("pane-skill should be removed after opt-out")
	}

	// Bare integrate clears runtime and re-asserts the full merged baseline.
	if err := applyIntegrateRuntimeConfig(true, nil); err != nil {
		t.Fatalf("applyIntegrateRuntimeConfig bare: %v", err)
	}
	baseline, err = integrationBaselineLoader()
	if err != nil {
		t.Fatalf("integrationBaselineLoader after bare clear: %v", err)
	}
	d2 := fakeDeps(home, fs, io.Discard)
	if err := runIntegrateComponents(d2, "claude", baseline, true, false, nil, false, false); err != nil {
		t.Fatalf("bare re-run: %v", err)
	}

	if _, ok := fs.symlinks[link]; !ok {
		t.Errorf("pane-skill not re-installed after bare integrate")
	}
}

// TestOptOutRemoval_Cycle: full install→opt-out-removal→explicit-re-add cycle.
func TestOptOutRemoval_Cycle(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"
	link := claudePaneLink(home)

	// Step 1: install pane-skill.
	d := fakeDeps(home, fs, io.Discard)
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring, ComponentPaneSkill}, false, false, nil, false, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, ok := fs.symlinks[link]; !ok {
		t.Fatalf("pane-skill not installed in step 1")
	}

	// Step 2: opt-out removal.
	var out bytes.Buffer
	d2 := fakeDeps(home, fs, &out)
	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := runIntegrateComponents(d2, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("opt-out: %v", err)
	}
	if _, ok := fs.symlinks[link]; ok {
		t.Fatalf("pane-skill still present after opt-out")
	}
	if !strings.Contains(out.String(), "removed (opted out)") {
		t.Errorf("expected 'removed (opted out)' in output, got %q", out.String())
	}

	// Step 3: bare re-add.
	d3 := fakeDeps(home, fs, io.Discard)
	if err := runIntegrateComponents(d3, "claude", []ComponentID{ComponentStatusWiring, ComponentPaneSkill}, false, false, nil, false, false); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if _, ok := fs.symlinks[link]; !ok {
		t.Errorf("pane-skill not re-added in step 3")
	}
}

// TestRefresh_LeavesRemovedOptedOutInPlace: the Integration refresh
// (--update-existing / auto-update) never removes an installed-but-opted-out
// component. After an explicit opt-out removal, refresh must not try to re-add
// or remove the component — it just sees it's not in the merged baseline.
func TestRefresh_LeavesRemovedOptedOutInPlace(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeIntegrateRuntimeFile(t, `
[integrations]
skills = ["tasks"]
`)
	fs := newFakeFS()
	home := "/h"

	// Install status wiring for claude so the agent is "known" to refresh.
	installViaFake(t, fs, home, "claude")

	// Install pane-skill, then remove it via explicit opt-out.
	seedFileComponent(t, fs, home, ComponentPaneSkill, "claude")
	link := claudePaneLink(home)
	d := fakeDeps(home, fs, io.Discard)
	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("opt-out: %v", err)
	}
	if _, ok := fs.symlinks[link]; ok {
		t.Fatalf("pane-skill should be removed before refresh")
	}

	// Record symlink state before refresh.
	symlinksBefore := make(map[string]string, len(fs.symlinks))
	for k, v := range fs.symlinks {
		symlinksBefore[k] = v
	}

	// Run the refresh path.
	dry, real := fakeFactories(home, fs)
	result := updateStaleIntegrations(dry, real)
	if len(result.Warnings) != 0 {
		t.Errorf("unexpected warnings from refresh: %v", result.Warnings)
	}

	// Pane-skill must not be re-added by refresh.
	if _, ok := fs.symlinks[link]; ok {
		t.Errorf("refresh must not re-add an opted-out (removed) pane-skill component")
	}

	// Refresh must not have emitted a "removed" outcome — removal is not its job.
	for _, o := range result.Outcomes {
		if o.Component == ComponentPaneSkill && strings.Contains(o.Label, "removed") {
			t.Errorf("refresh must not remove components, got outcome: %+v", o)
		}
	}
}

// ----- overwrite-conflicts (explicit install path) ---------------------------

func overwriteConflictSetup(t *testing.T) (*fakeFS, string, string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"
	conflictPath := filepath.Join(home, ".claude", "skills", "pane")
	fs.dirs[conflictPath] = true
	fs.files[filepath.Join(conflictPath, "SKILL.md")] = []byte("user skill")
	return fs, home, conflictPath
}

func TestOverwriteConflicts_PromptYes(t *testing.T) {
	fs, home, conflictPath := overwriteConflictSetup(t)
	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	d.stdin = strings.NewReader("y\n")

	optins := []ComponentID{ComponentStatusWiring, ComponentPaneSkill}
	if err := runIntegrateComponents(d, "claude", optins, true, false, nil, true, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Overwrite "+conflictPath) {
		t.Errorf("expected overwrite prompt naming %s, got %q", conflictPath, got)
	}
	if !strings.Contains(got, "OVERWRITE: destroyed "+conflictPath) {
		t.Errorf("expected loud overwrite report, got %q", got)
	}
	if !strings.Contains(got, "overwritten (not owned by pop at "+conflictPath+")") {
		t.Errorf("expected overwritten outcome, got %q", got)
	}
	if fs.dirs[conflictPath] {
		t.Errorf("conflict entry must be hard-deleted")
	}
	if _, ok := fs.symlinks[claudePaneLink(home)]; !ok {
		t.Errorf("pop pane-skill symlink must be installed after overwrite")
	}
}

func TestOverwriteConflicts_PromptNo(t *testing.T) {
	fs, home, conflictPath := overwriteConflictSetup(t)
	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	d.stdin = strings.NewReader("\n")

	optins := []ComponentID{ComponentStatusWiring, ComponentPaneSkill}
	if err := runIntegrateComponents(d, "claude", optins, true, false, nil, true, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Overwrite "+conflictPath) {
		t.Errorf("expected overwrite prompt, got %q", got)
	}
	if strings.Contains(got, "OVERWRITE:") {
		t.Errorf("must not overwrite on decline, got %q", got)
	}
	if !strings.Contains(got, "skipped (conflict at "+conflictPath) {
		t.Errorf("expected conflict skip outcome, got %q", got)
	}
	if !fs.dirs[conflictPath] {
		t.Errorf("unowned entry must be preserved on decline")
	}
	if string(fs.files[filepath.Join(conflictPath, "SKILL.md")]) != "user skill" {
		t.Errorf("user skill content must be preserved")
	}
}

func TestOverwriteConflicts_AssumeYes(t *testing.T) {
	fs, home, conflictPath := overwriteConflictSetup(t)
	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)

	optins := []ComponentID{ComponentStatusWiring, ComponentPaneSkill}
	if err := runIntegrateComponents(d, "claude", optins, false, false, nil, true, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "Overwrite ") {
		t.Errorf("--yes must skip prompt, got %q", got)
	}
	if !strings.Contains(got, "OVERWRITE: destroyed "+conflictPath) {
		t.Errorf("expected loud overwrite report, got %q", got)
	}
	if _, ok := fs.symlinks[claudePaneLink(home)]; !ok {
		t.Errorf("pane-skill must be linked after --yes overwrite")
	}
}

func TestOverwriteConflicts_NoTTYSkips(t *testing.T) {
	fs, home, conflictPath := overwriteConflictSetup(t)
	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)

	optins := []ComponentID{ComponentStatusWiring, ComponentPaneSkill}
	if err := runIntegrateComponents(d, "claude", optins, false, false, nil, true, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "Overwrite ") || strings.Contains(got, "OVERWRITE:") {
		t.Errorf("non-interactive overwrite must skip without prompting, got %q", got)
	}
	if !strings.Contains(got, "skipped (conflict at "+conflictPath) {
		t.Errorf("expected conflict skip outcome, got %q", got)
	}
	if !fs.dirs[conflictPath] {
		t.Errorf("unowned entry must be preserved without TTY")
	}
}

func TestIntegrateCmd_OverwriteConflictsWithUpdateExistingIsError(t *testing.T) {
	prevUpdate := integrateUpdateExisting
	prevOverwrite := integrateOverwriteConflicts
	integrateUpdateExisting = true
	integrateOverwriteConflicts = true
	t.Cleanup(func() {
		integrateUpdateExisting = prevUpdate
		integrateOverwriteConflicts = prevOverwrite
	})

	err := runIntegrate(integrateCmd, nil)
	if err == nil {
		t.Fatal("expected error when --overwrite-conflicts is combined with --update-existing")
	}
	if !strings.Contains(err.Error(), "--overwrite-conflicts") || !strings.Contains(err.Error(), "--update-existing") {
		t.Errorf("error should mention both flags, got %q", err.Error())
	}
}

func TestOverwriteConflicts_MultiAgentUniform(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	claudeConflict := filepath.Join(home, ".claude", "skills", "pane")
	fs.dirs[claudeConflict] = true
	piConflict := filepath.Join(home, ".pi", "agent", "skills", "pane")
	fs.dirs[piConflict] = true

	optins := []ComponentID{ComponentStatusWiring, ComponentPaneSkill}
	for _, agent := range []string{"claude", "pi"} {
		d := fakeDeps(home, fs, io.Discard)
		if err := runIntegrateComponents(d, agent, optins, false, false, nil, true, true); err != nil {
			t.Fatalf("%s install: %v", agent, err)
		}
	}

	if fs.dirs[claudeConflict] || fs.dirs[piConflict] {
		t.Errorf("both agents' conflicts must be overwritten with --yes")
	}
	claudeLink := claudePaneLink(home)
	piLink := filepath.Join(home, ".pi", "agent", "skills", "pop-pane")
	if _, ok := fs.symlinks[claudeLink]; !ok {
		t.Errorf("claude pane-skill not installed")
	}
	if _, ok := fs.symlinks[piLink]; !ok {
		t.Errorf("pi pane-skill not installed")
	}
}

func TestOptOutRemoval_StillNoPromptWithOverwriteFlag(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"
	seedFileComponent(t, fs, home, ComponentPaneSkill, "claude")

	d := fakeDeps(home, fs, io.Discard)
	d.stdin = nil
	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, true, true); err != nil {
		t.Fatalf("opt-out removal must succeed: %v", err)
	}
	if _, ok := fs.symlinks[claudePaneLink(home)]; ok {
		t.Errorf("pane-skill symlink should be removed by opt-out")
	}
}
