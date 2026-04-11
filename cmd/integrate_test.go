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
	for _, agent := range []string{"claude", "pi", "opencode"} {
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
	for _, agent := range []string{"claude", "pi", "opencode"} {
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
			stalePath: filepath.Join(".claude", "commands", "pop", "pane.md"),
		},
		{
			agent:     "pi",
			stalePath: filepath.Join(".pi", "agent", "extensions", "pop-status-sync.ts"),
		},
		{
			agent:     "opencode",
			stalePath: filepath.Join(".config", "opencode", "plugins", "pop-status-sync.ts"),
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
			fs.files[fullPath] = []byte("stale bytes that differ from embedded content")

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
	// existing pop hooks are found — even though the commands/pop/ directory
	// contains the current, unchanged skill files.
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	// Delete the commands dir so only settings.json remains as a signal.
	commandsDir := filepath.Join("/h", ".claude", "commands", "pop")
	for path := range fs.files {
		if strings.HasPrefix(path, commandsDir+string(filepath.Separator)) {
			delete(fs.files, path)
		}
	}

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

	// Corrupt one file so claude shows as changed.
	stalePath := filepath.Join("/h", ".claude", "commands", "pop", "pane.md")
	if _, exists := fs.files[stalePath]; !exists {
		t.Fatalf("seed install did not produce %s", stalePath)
	}
	fs.files[stalePath] = []byte("STALE")

	dry, real := fakeFactories("/h", fs)

	warnings := ensureIntegrationsForRevisionWith("rev2", dry, real)
	if warnings != nil {
		t.Errorf("expected no warnings on successful update, got %v", warnings)
	}
	// File should now match embedded content again.
	embedded, err := skillFiles.ReadFile("skills/pop/pane.md")
	if err != nil {
		t.Fatalf("read embedded pane.md: %v", err)
	}
	if !bytes.Equal(fs.files[stalePath], embedded) {
		t.Errorf("stale file was not updated to embedded bytes")
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

	stalePath := filepath.Join("/h", ".claude", "commands", "pop", "pane.md")
	fs.files[stalePath] = []byte("STALE")

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
	clauseStale := filepath.Join("/h", ".claude", "commands", "pop", "pane.md")
	fs.files[clauseStale] = []byte("STALE")

	// Make pi stale AND fail its write.
	piExtPath := filepath.Join("/h", ".pi", "agent", "extensions", "pop-status-sync.ts")
	fs.files[piExtPath] = []byte("STALE PI EXT")
	fs.writeErr[piExtPath] = errors.New("pi write failure")

	dry, real := fakeFactories("/h", fs)
	warnings := ensureIntegrationsForRevisionWith("rev4", dry, real)

	// claude should have updated cleanly.
	embedded, _ := skillFiles.ReadFile("skills/pop/pane.md")
	if !bytes.Equal(fs.files[clauseStale], embedded) {
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
	// integrationAgents (claude, pi, opencode). opencode isn't installed
	// and must not appear.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	installViaFake(t, fs, "/h", "pi")

	// Make both stale.
	clauseStale := filepath.Join("/h", ".claude", "commands", "pop", "pane.md")
	fs.files[clauseStale] = []byte("STALE claude")
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

	stalePath := filepath.Join("/h", ".claude", "commands", "pop", "pane.md")
	fs.files[stalePath] = []byte("STALE")
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
