package cmd

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

//go:embed skills/pop/*.md
var skillFiles embed.FS

var installSkills bool

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install pop integrations",
	Long: `Install pop integrations for other tools.

  --skills    Install Claude Code skills to ~/.claude/commands/pop/`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().BoolVar(&installSkills, "skills", false, "Install Claude Code skills to ~/.claude/commands/pop/")
}

func runInstall(cmd *cobra.Command, args []string) error {
	if !installSkills {
		return cmd.Help()
	}

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
