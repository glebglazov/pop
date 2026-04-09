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

// ----- Fake filesystem -------------------------------------------------------

// fakeFS is a tiny in-memory filesystem used to drive integrateDeps in tests.
// It records exact paths and contents so tests can assert directory layout.
type fakeFS struct {
	files     map[string][]byte
	dirs      map[string]bool
	readErr   map[string]error // path → error to return from readFile
	writeErr  map[string]error // path → error to return from writeFile
	mkdirErr  map[string]error
	removeErr map[string]error
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files:     map[string][]byte{},
		dirs:      map[string]bool{},
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
	for k := range f.files {
		if k == path || strings.HasPrefix(k, path+string(filepath.Separator)) {
			delete(f.files, k)
		}
	}
	return nil
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

func TestIntegrateClaude_WritesExactDirectoryLayout(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Embedded skill files should be written as ~/.claude/commands/pop/<name>.md
	skillEntries, _ := skillFiles.ReadDir("skills/pop")
	for _, entry := range skillEntries {
		if entry.IsDir() {
			continue
		}
		want := filepath.Join("/h", ".claude", "commands", "pop", entry.Name())
		if _, ok := fs.files[want]; !ok {
			t.Errorf("expected skill at %s, files written: %v", want, sortedKeys(fs.files))
		}
	}

	// settings.json should be written.
	settingsPath := filepath.Join("/h", ".claude", "settings.json")
	if _, ok := fs.files[settingsPath]; !ok {
		t.Errorf("expected settings.json at %s", settingsPath)
	}

	// Commands directory should have been removeAll'd before recreation
	// (cleaning step). The mkdirAll for the commands dir should be present.
	commandsDir := filepath.Join("/h", ".claude", "commands", "pop")
	if !fs.dirs[commandsDir] {
		t.Errorf("expected mkdirAll of %s", commandsDir)
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

func TestIntegratePi_WritesSkillDirectoryStructure(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// For each embedded source skill, expect a directory at
	//   ~/.pi/agent/skills/pop-<basename>/
	// containing a SKILL.md file.
	skillEntries, _ := skillFiles.ReadDir("skills/pop")
	for _, entry := range skillEntries {
		if entry.IsDir() {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		piName := "pop-" + base
		dir := filepath.Join("/h", ".pi", "agent", "skills", piName)
		skillFile := filepath.Join(dir, "SKILL.md")

		if !fs.dirs[dir] {
			t.Errorf("expected mkdirAll of %s", dir)
		}
		if _, ok := fs.files[skillFile]; !ok {
			t.Errorf("expected skill file at %s, files: %v", skillFile, sortedKeys(fs.files))
		}
	}
}

func TestIntegratePi_InjectsFrontmatterName(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Every installed pi skill must have a `name:` field that matches its
	// parent directory (per the Agent Skills spec pi enforces).
	skillEntries, _ := skillFiles.ReadDir("skills/pop")
	for _, entry := range skillEntries {
		if entry.IsDir() {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		piName := "pop-" + base
		path := filepath.Join("/h", ".pi", "agent", "skills", piName, "SKILL.md")
		content := string(fs.files[path])
		wantLine := "name: " + piName
		if !strings.Contains(content, wantLine) {
			t.Errorf("expected %q in %s frontmatter, got:\n%s", wantLine, path, content)
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

func TestIntegratePi_OverwritesPriorSkillDirectory(t *testing.T) {
	// Pre-seed a stale file inside the pop-pane skill directory and verify
	// it gets cleaned out when re-installing — guards against rename rot.
	fs := newFakeFS()
	stalePath := filepath.Join("/h", ".pi", "agent", "skills", "pop-pane", "stale.md")
	fs.files[stalePath] = []byte("old garbage")

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := fs.files[stalePath]; exists {
		t.Errorf("stale file %s should have been removed by removeAll, files: %v",
			stalePath, sortedKeys(fs.files))
	}

	// Fresh SKILL.md should be present.
	freshPath := filepath.Join("/h", ".pi", "agent", "skills", "pop-pane", "SKILL.md")
	if _, ok := fs.files[freshPath]; !ok {
		t.Errorf("expected fresh SKILL.md at %s", freshPath)
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

func TestIntegratePi_SkillWriteError(t *testing.T) {
	fs := newFakeFS()
	// Fail the first SKILL.md write we attempt.
	skillEntries, _ := skillFiles.ReadDir("skills/pop")
	var first string
	for _, entry := range skillEntries {
		if !entry.IsDir() {
			base := strings.TrimSuffix(entry.Name(), ".md")
			first = filepath.Join("/h", ".pi", "agent", "skills", "pop-"+base, "SKILL.md")
			break
		}
	}
	if first == "" {
		t.Skip("no embedded skills to test against")
	}
	fs.writeErr[first] = os.ErrPermission

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "pi"); err == nil {
		t.Fatal("expected error from skill write failure")
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
	if !bytes.Contains(contents, []byte(`$` + "`" + `pop pane set-status`)) {
		t.Error("plugin content does not look like the pop status-sync TS file")
	}
	if !bytes.Equal(contents, opencodeExtensionFile) {
		t.Error("plugin on disk should match the embedded source byte-for-byte")
	}
}

func TestIntegrateOpencode_WritesSkillFiles(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// For each embedded source skill, expect a file at
	//   ~/.config/opencode/agent/pop-<basename>.md
	skillEntries, _ := skillFiles.ReadDir("skills/pop")
	for _, entry := range skillEntries {
		if entry.IsDir() {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		opencodeName := "pop-" + base
		skillFile := filepath.Join("/h", ".config", "opencode", "agent", opencodeName+".md")

		if _, ok := fs.files[skillFile]; !ok {
			t.Errorf("expected skill file at %s, files: %v", skillFile, sortedKeys(fs.files))
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

func TestIntegrateOpencode_SkillWriteError(t *testing.T) {
	fs := newFakeFS()
	// Fail the first skill write we attempt.
	skillEntries, _ := skillFiles.ReadDir("skills/pop")
	var first string
	for _, entry := range skillEntries {
		if !entry.IsDir() {
			base := strings.TrimSuffix(entry.Name(), ".md")
			first = filepath.Join("/h", ".config", "opencode", "agent", "pop-"+base+".md")
			break
		}
	}
	if first == "" {
		t.Skip("no embedded skills to test against")
	}
	fs.writeErr[first] = os.ErrPermission

	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode"); err == nil {
		t.Fatal("expected error from skill write failure")
	}
}

func TestIntegrateOpencode_CreatesAgentDirectory(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "opencode"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	agentDir := filepath.Join("/h", ".config", "opencode", "agent")
	if !fs.dirs[agentDir] {
		t.Errorf("expected mkdirAll of %s", agentDir)
	}
}

func TestIntegrateOpencode_AgentNameIsCaseInsensitive(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "OpEnCoDe"); err != nil {
		t.Errorf("expected case-insensitive agent matching, got error: %v", err)
	}
}
