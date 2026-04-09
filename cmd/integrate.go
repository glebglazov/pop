package cmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed skills/pop/*.md
var skillFiles embed.FS

//go:embed extensions/pi/pop-status-sync.ts
var piExtensionFile []byte

//go:embed extensions/opencode/pop-status-sync.ts
var opencodeExtensionFile []byte

// integrateDeps holds the filesystem operations the integrate command depends
// on. Production code uses defaultIntegrateDeps; tests inject mocks.
type integrateDeps struct {
	userHomeDir func() (string, error)
	readFile    func(string) ([]byte, error)
	writeFile   func(string, []byte, os.FileMode) error
	mkdirAll    func(string, os.FileMode) error
	removeAll   func(string) error
	stdout      io.Writer
}

func defaultIntegrateDeps() *integrateDeps {
	return &integrateDeps{
		userHomeDir: os.UserHomeDir,
		readFile:    os.ReadFile,
		writeFile:   os.WriteFile,
		mkdirAll:    os.MkdirAll,
		removeAll:   os.RemoveAll,
		stdout:      os.Stdout,
	}
}

var integrateCmd = &cobra.Command{
	Use:   "integrate <agent>",
	Short: "Install pop integrations for a coding agent",
	Long: `Install pop integrations (skills + pane-status hooks) for a coding agent.

Supported agents:
  claude    Install slash commands at ~/.claude/commands/pop/ and pane
            monitoring hooks in ~/.claude/settings.json.
  pi        Install skills at ~/.pi/agent/skills/pop-<name>/SKILL.md and a
            pane monitoring extension at
            ~/.pi/agent/extensions/pop-status-sync.ts.
  opencode  Install skills at ~/.config/opencode/agent/pop-<name>.md and a
            pane monitoring plugin at
            ~/.config/opencode/plugins/pop-status-sync.ts.

Re-running the command for an agent is idempotent: existing pop integration
files for that agent are replaced with the current versions, and unrelated
hooks/skills are preserved.`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"claude", "pi", "opencode"},
	RunE:      runIntegrate,
}

func init() {
	rootCmd.AddCommand(integrateCmd)
}

func runIntegrate(cmd *cobra.Command, args []string) error {
	return runIntegrateWith(defaultIntegrateDeps(), args[0])
}

func runIntegrateWith(d *integrateDeps, agent string) error {
	switch strings.ToLower(agent) {
	case "claude":
		return integrateClaude(d)
	case "pi":
		return integratePi(d)
	case "opencode":
		return integrateOpencode(d)
	default:
		return fmt.Errorf("unknown agent %q (expected: claude, pi, opencode)", agent)
	}
}

// ----- Claude integration ----------------------------------------------------

// popHooks defines the hook commands installed into Claude's settings.json.
// Each entry is a (event, command) pair; the matcher is left empty so the
// hook fires for every tool / event.
var popHooks = []struct {
	event   string
	command string
}{
	{"SessionStart", "pop pane set-status idle 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-status working 2>/dev/null || true"},
	{"PreToolUse", "pop pane set-status working 2>/dev/null || true"},
	{"Stop", "pop pane set-status unread 2>/dev/null || true"},
	{"Notification", "pop pane set-status unread 2>/dev/null || true"},
}

func integrateClaude(d *integrateDeps) error {
	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if err := installClaudeCommands(d, home); err != nil {
		return err
	}
	if err := installClaudeHooks(d, home); err != nil {
		return err
	}
	return nil
}

// installClaudeCommands writes the embedded skills as Claude slash commands
// under ~/.claude/commands/pop/. The directory is wiped first so removed
// skills do not linger.
func installClaudeCommands(d *integrateDeps, home string) error {
	commandsDir := filepath.Join(home, ".claude", "commands", "pop")
	if err := d.removeAll(commandsDir); err != nil {
		return fmt.Errorf("failed to clean %s: %w", commandsDir, err)
	}
	if err := d.mkdirAll(commandsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", commandsDir, err)
	}

	entries, err := skillFiles.ReadDir("skills/pop")
	if err != nil {
		return fmt.Errorf("failed to read embedded skills: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := skillFiles.ReadFile("skills/pop/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read skill %s: %w", entry.Name(), err)
		}
		dest := filepath.Join(commandsDir, entry.Name())
		if err := d.writeFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  installed /pop:%s\n", strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	return nil
}

// installClaudeHooks merges pop's hook entries into ~/.claude/settings.json,
// preserving any unrelated existing hooks. Old pop hooks are removed first
// (matched via isPopHook) so re-running the command is idempotent.
func installClaudeHooks(d *integrateDeps, home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

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
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}

	for _, h := range popHooks {
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
		fmt.Fprintf(d.stdout, "Installed %d hook(s) in %s\n", len(popHooks), settingsPath)
	}
	return nil
}

// ----- Pi integration --------------------------------------------------------

func integratePi(d *integrateDeps) error {
	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if err := installPiExtension(d, home); err != nil {
		return err
	}
	if err := installPiSkills(d, home); err != nil {
		return err
	}
	return nil
}

// installPiExtension writes the embedded pi extension TypeScript file. Pi
// auto-discovers any *.ts file under ~/.pi/agent/extensions/ at startup.
func installPiExtension(d *integrateDeps, home string) error {
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

// installPiSkills writes each embedded skill as a pi skill directory under
// ~/.pi/agent/skills/pop-<basename>/SKILL.md. Pi requires the frontmatter
// `name` field to match the parent directory; we inject it on install so the
// source files stay agent-agnostic.
func installPiSkills(d *integrateDeps, home string) error {
	entries, err := skillFiles.ReadDir("skills/pop")
	if err != nil {
		return fmt.Errorf("failed to read embedded skills: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := skillFiles.ReadFile("skills/pop/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read skill %s: %w", entry.Name(), err)
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		piName := "pop-" + base
		dir := filepath.Join(home, ".pi", "agent", "skills", piName)
		// Wipe any prior install of this exact skill so a renamed file does
		// not leave a stale SKILL.md behind.
		if err := d.removeAll(dir); err != nil {
			return fmt.Errorf("failed to clean %s: %w", dir, err)
		}
		if err := d.mkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
		dest := filepath.Join(dir, "SKILL.md")
		content := injectFrontmatterName(string(data), piName)
		if err := d.writeFile(dest, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  installed pi skill %s\n", piName)
		}
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

// ----- Opencode integration --------------------------------------------------

func integrateOpencode(d *integrateDeps) error {
	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if err := installOpencodePlugin(d, home); err != nil {
		return err
	}
	if err := installOpencodeSkills(d, home); err != nil {
		return err
	}
	return nil
}

// installOpencodePlugin writes the embedded opencode plugin TypeScript file.
// Opencode auto-discovers any *.ts file under ~/.config/opencode/plugins/ at startup.
func installOpencodePlugin(d *integrateDeps, home string) error {
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

// installOpencodeSkills writes each embedded skill as an opencode agent markdown
// file under ~/.config/opencode/agent/pop-<basename>.md.
func installOpencodeSkills(d *integrateDeps, home string) error {
	entries, err := skillFiles.ReadDir("skills/pop")
	if err != nil {
		return fmt.Errorf("failed to read embedded skills: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := skillFiles.ReadFile("skills/pop/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read skill %s: %w", entry.Name(), err)
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		opencodeName := "pop-" + base
		agentDir := filepath.Join(home, ".config", "opencode", "agent")
		if err := d.mkdirAll(agentDir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", agentDir, err)
		}
		dest := filepath.Join(agentDir, opencodeName+".md")
		if err := d.writeFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  installed opencode agent %s\n", opencodeName)
		}
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
// the pop pane-monitoring commands.
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
		if cmd, _ := hMap["command"].(string); strings.Contains(cmd, "pop monitor") || strings.Contains(cmd, "pop pane set-status") {
			return true
		}
	}
	return false
}
