package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Initialize or extend the pop configuration",
	Long: `Interactively set up the pop config file by adding project directory patterns.

If a config already exists, shows current patterns and offers to add more.
Opens a TUI for entering path patterns with tab completion and live preview.

Example:
  pop configure`,
	RunE: runConfigure,
}

func init() {
	rootCmd.AddCommand(configureCmd)
}

// configureDeps holds dependencies for the init command
type configureDeps struct {
	FS          deps.FileSystem
	Stdin       io.Reader
	Stdout      io.Writer
	PickDir     func() (ui.ConfigurePickerResult, error)
	ShowWelcome bool // show welcome message (when triggered from select)
}

func defaultConfigureDeps() *configureDeps {
	return &configureDeps{
		FS:     deps.NewRealFileSystem(),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		PickDir: func() (ui.ConfigurePickerResult, error) {
			expandFn := func(pattern string) []string {
				tmp := &config.Config{Projects: []config.ProjectEntry{{Path: pattern}}}
				paths, err := tmp.ExpandProjects()
				if err != nil {
					return nil
				}
				result := make([]string, len(paths))
				for i, p := range paths {
					result[i] = p.Path
				}
				return result
			}
			return ui.RunConfigurePicker(expandFn)
		},
	}
}

func runConfigure(cmd *cobra.Command, args []string) error {
	return runConfigureWith(defaultConfigureDeps())
}

func runConfigureWith(d *configureDeps) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = &config.Config{}
	}

	scanner := bufio.NewScanner(d.Stdin)

	if d.ShowWelcome {
		fmt.Fprintln(d.Stdout, "No config file found. Let's set one up!")
		fmt.Fprintln(d.Stdout, "Enter a directory pattern (e.g. ~/Dev/*) to add your projects.")
		fmt.Fprintln(d.Stdout, "You can re-run this later with: pop configure")
		fmt.Fprintln(d.Stdout)
		if !confirmY(scanner, d.Stdout, "Continue?") {
			return nil
		}
	}

	if len(cfg.Projects) > 0 {
		fmt.Fprintf(d.Stdout, "Config found at %s\n", cfgPath)
		fmt.Fprintf(d.Stdout, "Current patterns:\n")
		for _, p := range cfg.Projects {
			fmt.Fprintf(d.Stdout, "  - %s\n", p.Path)
		}
		fmt.Fprintln(d.Stdout)

		if !confirm(scanner, d.Stdout, "Add another directory?") {
			return nil
		}
	}

	for {
		result, err := d.PickDir()
		if err != nil {
			return err
		}
		if result.Cancelled || result.Path == "" {
			break
		}

		entry := config.ProjectEntry{
			Path:         result.Path,
			DisplayDepth: result.DisplayDepth,
		}

		count := countMatches(entry.Path)
		depthInfo := ""
		if entry.DisplayDepth > 1 {
			depthInfo = fmt.Sprintf(" (depth: %d)", entry.DisplayDepth)
		}
		if count == 0 {
			fmt.Fprintf(d.Stdout, "  %s%s — no projects found\n", entry.Path, depthInfo)
		} else {
			fmt.Fprintf(d.Stdout, "  %s%s — found %d projects\n", entry.Path, depthInfo, count)
		}

		cfg.Projects = append(cfg.Projects, entry)

		if !confirm(scanner, d.Stdout, "Add another directory?") {
			break
		}
	}

	if len(cfg.Projects) == 0 {
		return nil
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	dir := filepath.Dir(cfgPath)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := d.FS.WriteFile(cfgPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Fprintf(d.Stdout, "\nConfig written to %s\n", cfgPath)

	return nil
}

func confirm(scanner *bufio.Scanner, w io.Writer, prompt string) bool {
	fmt.Fprintf(w, "%s [y/N]: ", prompt)
	if !scanner.Scan() {
		return false
	}
	return strings.ToLower(strings.TrimSpace(scanner.Text())) == "y"
}

func confirmY(scanner *bufio.Scanner, w io.Writer, prompt string) bool {
	fmt.Fprintf(w, "%s [Y/n]: ", prompt)
	if !scanner.Scan() {
		return true
	}
	return strings.ToLower(strings.TrimSpace(scanner.Text())) != "n"
}

func countMatches(pattern string) int {
	tmp := &config.Config{Projects: []config.ProjectEntry{{Path: pattern}}}
	paths, err := tmp.ExpandProjects()
	if err != nil {
		return 0
	}
	return len(paths)
}
