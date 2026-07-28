package integrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ----- Claude integration ----------------------------------------------------

type hookSpec struct {
	event   string
	command string
}

// popHooks defines the hook commands installed into Claude's settings.json.
// Each entry is a (event, command) pair; the matcher is left empty so the
// hook fires for every tool / event.
//
// The topic hook is a *separate* UserPromptSubmit entry alongside the
// set-status one (ADR 0023): it pipes the same payload to `set-topic --derive`
// to derive a pane topic from the prompt. SessionStart also clears the Topic
// so a new agent session in the same pane can re-derive on its first prompt.
// It installs whenever the core status wiring installs — no extra opt-in —
// and rides the same idempotent install/remove/refresh paths (both commands
// match isPopHookCommand).
var popHooks = []hookSpec{
	{"SessionStart", "pop pane set-status clear 2>/dev/null || true"},
	{"SessionStart", "pop pane set-topic --clear 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-status working 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-topic --derive 2>/dev/null || true"},
	{"PreToolUse", "pop pane set-status working 2>/dev/null || true"},
	{"Stop", "pop pane set-status unread 2>/dev/null || true"},
	{"Notification", "pop pane set-status unread 2>/dev/null || true"},
}

// installClaudeHooks merges pop's hook entries into ~/.claude/settings.json,
// preserving any unrelated existing hooks. Old pop hooks are removed first
// (matched via isPopHook) so re-running the command is idempotent.
func installClaudeHooks(r *run, home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	return installJSONHooks(r, settingsPath, popHooks)
}

func installJSONHooks(r *run, settingsPath string, hooksToInstall []hookSpec) error {
	d := r.deps
	settings := make(map[string]interface{})
	data, err := d.readFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	// Strip any previously installed pop hooks before adding the current set.
	for event, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		cleaned := removePopHooks(eventHooks)
		// Dry-run "installed" detection for claude: settings.json exists for
		// every claude user, so file-presence is not a reliable signal that
		// pop is installed. Finding any existing pop hooks is — they could
		// only have gotten there via a prior `pop integrate claude` run.
		if r.dryRun && len(cleaned) < len(eventHooks) {
			r.installed = true
		}
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}

	for _, h := range hooksToInstall {
		hookEntry := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": h.command,
				},
			},
		}
		eventHooks, _ := hooks[h.event].([]interface{})
		eventHooks = append(eventHooks, hookEntry)
		hooks[h.event] = eventHooks
	}

	if err := d.mkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	if err := d.writeFile(settingsPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsPath, err)
	}

	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed %d hook(s) in %s\n", len(hooksToInstall), settingsPath)
	}
	return nil
}

// ----- Codex integration -----------------------------------------------------

var codexPopHooks = []hookSpec{
	{"SessionStart", "pop pane set-status clear 2>/dev/null || true"},
	{"SessionStart", "pop pane set-topic --clear 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-status working 2>/dev/null || true"},
	// Topic hook: a separate UserPromptSubmit entry alongside set-status,
	// riding core status wiring (ADR 0023). --label codex selects codex's
	// payload adapter; codex exposes no transcript_path.
	{"UserPromptSubmit", "pop pane set-topic --derive --label codex 2>/dev/null || true"},
	{"PreToolUse", "pop pane set-status working 2>/dev/null || true"},
	{"PermissionRequest", "pop pane set-status unread 2>/dev/null || true"},
	{"Stop", "pop pane set-status unread 2>/dev/null || true"},
}

func installCodexHooks(r *run, home string) error {
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	return installJSONHooks(r, hooksPath, codexPopHooks)
}

// ----- Pi integration --------------------------------------------------------

// installPiExtension writes the embedded pi extension TypeScript file. Pi
// auto-discovers any *.ts file under ~/.pi/agent/extensions/ at startup.
func installPiExtension(r *run, home string) error {
	d := r.deps
	extDir := filepath.Join(home, ".pi", "agent", "extensions")
	if err := d.mkdirAll(extDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", extDir, err)
	}
	extPath := filepath.Join(extDir, "pop-status-sync.ts")
	if err := d.writeFile(extPath, piExtensionFile, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", extPath, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed pi extension at %s\n", extPath)
	}
	return nil
}

// injectFrontmatterName guarantees the YAML frontmatter contains a `name:`
// field set to the given value. If the file already has a name, it is
// replaced. If there is no frontmatter at all, one is created.
func injectFrontmatterName(content, name string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		// No frontmatter — wrap the content in one.
		return fmt.Sprintf("---\nname: %s\n---\n%s", name, content)
	}
	// Find the closing `---`.
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		// Malformed frontmatter — leave it alone.
		return content
	}
	// Replace existing name: line if present.
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "name:") {
			lines[i] = "name: " + name
			return strings.Join(lines, "\n")
		}
	}
	// Otherwise insert `name:` directly after the opening `---`.
	out := append([]string{lines[0], "name: " + name}, lines[1:]...)
	return strings.Join(out, "\n")
}

const popOwnedField = "pop-owned"

func injectOwnershipMarker(content string) string {
	return setFrontmatterField(content, popOwnedField, "true")
}

func setFrontmatterField(content, key, value string) string {
	field := key + ": " + value
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fmt.Sprintf("---\n%s\n---\n%s", field, content)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return content
	}
	prefix := key + ":"
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			lines[i] = field
			return strings.Join(lines, "\n")
		}
	}
	out := append([]string{lines[0], field}, lines[1:]...)
	return strings.Join(out, "\n")
}

func frontmatterHasOwnershipMarker(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	prefix := popOwnedField + ":"
	for i := 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "---" {
			return false
		}
		if strings.HasPrefix(t, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(t, prefix)) == "true"
		}
	}
	return false
}

// ----- Opencode integration --------------------------------------------------

// installOpencodePlugin writes the embedded opencode plugin TypeScript file.
// Opencode auto-discovers any *.ts file under ~/.config/opencode/plugins/ at startup.
func installOpencodePlugin(r *run, home string) error {
	d := r.deps
	pluginDir := filepath.Join(home, ".config", "opencode", "plugins")
	if err := d.mkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", pluginDir, err)
	}
	pluginPath := filepath.Join(pluginDir, "pop-status-sync.ts")
	if err := d.writeFile(pluginPath, opencodeExtensionFile, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", pluginPath, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed opencode plugin at %s\n", pluginPath)
	}
	return nil
}

// ----- Cursor integration ----------------------------------------------------

type cursorHookSpec struct {
	event   string
	command string
}

// cursorPopHooks defines the hook commands installed into Cursor's hooks.json.
// Event names follow the Cursor CLI hooks schema (camelCase).
var cursorPopHooks = []cursorHookSpec{
	{"sessionStart", "pop pane set-status clear --label cursor 2>/dev/null || true"},
	{"sessionStart", "pop pane set-topic --clear 2>/dev/null || true"},
	{"beforeSubmitPrompt", "pop pane set-status working --label cursor 2>/dev/null || true"},
	// Topic hook: a separate beforeSubmitPrompt entry alongside set-status,
	// riding core status wiring (ADR 0023). --label cursor selects cursor's
	// payload adapter; cursor exposes no transcript_path.
	{"beforeSubmitPrompt", "pop pane set-topic --derive --label cursor 2>/dev/null || true"},
	{"preToolUse", "pop pane set-status working --label cursor 2>/dev/null || true"},
	{"afterAgentResponse", "pop pane set-status unread --label cursor 2>/dev/null || true"},
	{"stop", "pop pane set-status unread --label cursor 2>/dev/null || true"},
}

// installCursorHooks merges pop's hook entries into ~/.cursor/hooks.json,
// preserving any unrelated existing hooks. Old pop hooks are removed first
// (matched via isCursorPopHook) so re-running the command is idempotent.
func installCursorHooks(r *run, home string) error {
	d := r.deps
	hooksPath := filepath.Join(home, ".cursor", "hooks.json")

	settings := make(map[string]interface{})
	data, err := d.readFile(hooksPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", hooksPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", hooksPath, err)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	if _, ok := settings["version"]; !ok {
		settings["version"] = 1
	}

	for event, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		cleaned := removeCursorPopHooks(eventHooks)
		if r.dryRun && len(cleaned) < len(eventHooks) {
			r.installed = true
		}
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}

	for _, h := range cursorPopHooks {
		hookEntry := map[string]interface{}{
			"command": h.command,
		}
		eventHooks, _ := hooks[h.event].([]interface{})
		eventHooks = append(eventHooks, hookEntry)
		hooks[h.event] = eventHooks
	}

	if err := d.mkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to serialize hooks: %w", err)
	}

	if err := d.writeFile(hooksPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", hooksPath, err)
	}

	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed %d hook(s) in %s\n", len(cursorPopHooks), hooksPath)
	}
	return nil
}

// ----- Shared helpers --------------------------------------------------------

// removePopHooks filters out hook entries whose commands look like pop
// monitoring commands. Used to deduplicate when re-installing.
func removePopHooks(entries []interface{}) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		if !isPopHook(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// isPopHook returns true if any command in the hook entry references one of
// the pop pane-monitoring commands. Handles the nested Claude/Codex format.
func isPopHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	innerHooks, _ := entryMap["hooks"].([]interface{})
	for _, h := range innerHooks {
		hMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if cmd, _ := hMap["command"].(string); isPopHookCommand(cmd) {
			return true
		}
	}
	return false
}

// removeCursorPopHooks filters out Cursor-format hook entries whose commands
// look like pop monitoring commands.
func removeCursorPopHooks(entries []interface{}) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		if !isCursorPopHook(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// isCursorPopHook returns true if a Cursor-format hook entry references one
// of the pop pane-monitoring commands.
func isCursorPopHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	cmd, _ := entryMap["command"].(string)
	return isPopHookCommand(cmd)
}

func isPopHookCommand(cmd string) bool {
	return strings.Contains(cmd, "pop monitor") ||
		strings.Contains(cmd, "pop pane set-status") ||
		strings.Contains(cmd, "pop pane set-topic") ||
		strings.Contains(cmd, "pop-status")
}

