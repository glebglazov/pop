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
Opens a directory picker to browse and select project directories.

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
	PickDir     func() (string, bool, error) // returns (path, cancelled, error)
	ShowWelcome bool                          // show welcome message (when triggered from select)
}

func defaultConfigureDeps() *configureDeps {
	return &configureDeps{
		FS:     deps.NewRealFileSystem(),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		PickDir: func() (string, bool, error) {
			result, err := ui.RunDirPicker()
			return result.Path, result.Cancelled, err
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
		fmt.Fprintln(d.Stdout, "Browse to a directory whose children are your projects, then press Enter to select it.")
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
			fmt.Fprintf(d.Stdout, "  - %s\n", p)
		}
		fmt.Fprintln(d.Stdout)

		if !confirm(scanner, d.Stdout, "Add another directory?") {
			return nil
		}
	}

	for {
		path, cancelled, err := d.PickDir()
		if err != nil {
			return err
		}
		if cancelled || path == "" {
			break
		}

		pattern := toTildePattern(path)

		count := countMatches(pattern)
		if count == 0 {
			fmt.Fprintf(d.Stdout, "  %s — no projects found\n", pattern)
		} else {
			fmt.Fprintf(d.Stdout, "  %s — found %d projects\n", pattern, count)
		}

		cfg.Projects = append(cfg.Projects, pattern)

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

// toTildePattern converts an absolute path to a ~-prefixed glob pattern.
// e.g. /Users/foo/Dev → ~/Dev/*
func toTildePattern(absPath string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(absPath, home) {
		return "~" + absPath[len(home):] + "/*"
	}
	return absPath + "/*"
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
	tmp := &config.Config{Projects: []string{pattern}}
	paths, err := tmp.ExpandProjects()
	if err != nil {
		return 0
	}
	return len(paths)
}
