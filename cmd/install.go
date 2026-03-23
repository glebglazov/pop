package cmd

import (
	"embed"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

const popHookCommand = "pop monitor register $TMUX_PANE --source claude-code 2>/dev/null || true"

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

	// Build the hook entry we want to add
	popHook := map[string]interface{}{
		"matcher": "startup",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": popHookCommand,
			},
		},
	}

	// Get or create hooks.SessionStart
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	sessionStartRaw, _ := hooks["SessionStart"].([]interface{})

	// Check if our hook is already installed
	for _, entry := range sessionStartRaw {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		innerHooks, _ := entryMap["hooks"].([]interface{})
		for _, h := range innerHooks {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if cmd, _ := hMap["command"].(string); cmd == popHookCommand {
				fmt.Println("Hook already installed in " + settingsPath)
				return nil
			}
		}
	}

	// Append our hook
	sessionStartRaw = append(sessionStartRaw, popHook)
	hooks["SessionStart"] = sessionStartRaw

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
	out := buf.Bytes()

	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsPath, err)
	}

	fmt.Printf("Hook installed in %s\n", settingsPath)
	fmt.Println("Claude Code will auto-register panes for monitoring on startup.")
	return nil
}
