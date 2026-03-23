package cmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed skills/pop/*.md
var skillFiles embed.FS

var installSkills bool
var installHooks bool

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install pop integrations",
	Long: `Install pop integrations for other tools.

  --skills    Install Claude Code skills to ~/.claude/commands/pop/
  --hooks     Install Claude Code hooks for monitor auto-registration`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().BoolVar(&installSkills, "skills", false, "Install Claude Code skills to ~/.claude/commands/pop/")
	installCmd.Flags().BoolVar(&installHooks, "hooks", false, "Install Claude Code hooks for monitor auto-registration")
}

func runInstall(cmd *cobra.Command, args []string) error {
	if !installSkills && !installHooks {
		return cmd.Help()
	}

	if installSkills {
		if err := runInstallSkills(); err != nil {
			return err
		}
	}

	if installHooks {
		if err := runInstallHooks(); err != nil {
			return err
		}
	}

	return nil
}

func runInstallSkills() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// All pop skills live under ~/.claude/commands/pop/ → invoked as /pop:pane etc.
	// On install, we replace the entire directory so removed skills don't linger.
	destDir := filepath.Join(home, ".claude", "commands", "pop")
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("failed to clean %s: %w", destDir, err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", destDir, err)
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

		destPath := filepath.Join(destDir, entry.Name())
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}
		fmt.Printf("  installed /pop:%s\n", entry.Name()[:len(entry.Name())-3]) // strip .md
	}

	fmt.Println("\nSkills installed. Available as /pop:<name> in Claude Code.")
	return nil
}

// popHooks defines all Claude Code hooks needed for monitoring.
// Each entry maps a hook event to a hook config (matcher + command).
var popHooks = []struct {
	event   string
	matcher string
	command string
}{
	{"SessionStart", "startup", "pop monitor register $TMUX_PANE 2>/dev/null || true"},
	{"UserPromptSubmit", "", "pop monitor set-status $TMUX_PANE working 2>/dev/null || true"},
	{"PreToolUse", "", "pop monitor set-status $TMUX_PANE working 2>/dev/null || true"},
	{"Stop", "", "pop monitor set-status $TMUX_PANE needs_attention 2>/dev/null || true"},
	{"Notification", "", "pop monitor set-status $TMUX_PANE needs_attention 2>/dev/null || true"},
}

func runInstallHooks() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// Load existing settings or start fresh
	settings := make(map[string]interface{})
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	// Get or create hooks object
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	// Remove any existing pop hooks before installing current ones
	for event, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		hooks[event] = removePopHooks(eventHooks)
	}

	// Install current hooks
	for _, h := range popHooks {
		hookEntry := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": h.command,
				},
			},
		}
		if h.matcher != "" {
			hookEntry["matcher"] = h.matcher
		}

		eventHooks, _ := hooks[h.event].([]interface{})
		eventHooks = append(eventHooks, hookEntry)
		hooks[h.event] = eventHooks
	}

	// Write back
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsPath, err)
	}

	fmt.Printf("Installed %d hook(s) in %s\n", len(popHooks), settingsPath)
	return nil
}

// removePopHooks filters out hook entries whose commands contain "pop monitor"
func removePopHooks(entries []interface{}) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		if !isPopHook(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// isPopHook returns true if any command in the hook entry contains "pop monitor"
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
		if cmd, _ := hMap["command"].(string); strings.Contains(cmd, "pop monitor") {
			return true
		}
	}
	return false
}
