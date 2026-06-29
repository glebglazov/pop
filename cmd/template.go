package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/spf13/cobra"
)

type templateRuntimeDeps struct {
	Tmux       deps.Tmux
	LoadConfig func() (*config.Config, error)
	Getwd      func() (string, error)
}

func defaultTemplateRuntimeDeps() templateRuntimeDeps {
	return templateRuntimeDeps{
		Tmux: defaultTmux,
		LoadConfig: func() (*config.Config, error) {
			path := cfgFile
			if path == "" {
				path = config.DefaultConfigPath()
			}
			return config.Load(path)
		},
		Getwd: os.Getwd,
	}
}

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage session templates",
}

var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List session templates",
	Args:  cobra.NoArgs,
	RunE:  runTemplateList,
}

var templateApplyCmd = &cobra.Command{
	Use:   "apply <name>",
	Short: "Apply a session template to the current tmux session",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateApply,
}

func init() {
	rootCmd.AddCommand(templateCmd)
	templateCmd.AddCommand(templateListCmd)
	templateCmd.AddCommand(templateApplyCmd)
}

func runTemplateList(cmd *cobra.Command, args []string) error {
	d := defaultTemplateRuntimeDeps()
	cfg, err := d.LoadConfig()
	if err != nil {
		return err
	}
	return runTemplateListWith(cfg, cmd.OutOrStdout())
}

func runTemplateListWith(cfg *config.Config, out io.Writer) error {
	if cfg == nil {
		return nil
	}
	for _, tmpl := range cfg.SessionTemplates {
		if tmpl.Name == "" {
			continue
		}
		if _, err := fmt.Fprintln(out, tmpl.Name); err != nil {
			return err
		}
	}
	return nil
}

func runTemplateApply(cmd *cobra.Command, args []string) error {
	d := defaultTemplateRuntimeDeps()
	cfg, err := d.LoadConfig()
	if err != nil {
		return err
	}
	return runTemplateApplyWith(d, cfg, args[0])
}

func runTemplateApplyWith(d templateRuntimeDeps, cfg *config.Config, name string) error {
	tmpl, ok := findSessionTemplate(cfg, name)
	if !ok {
		return fmt.Errorf("session template %q not found", name)
	}
	window, pane, err := walkingSkeletonTemplateSpec(tmpl)
	if err != nil {
		return fmt.Errorf("session template %q: %w", name, err)
	}

	session := currentTmuxSessionWith(d.Tmux)
	if session == "" {
		return fmt.Errorf("not inside a tmux session")
	}
	dir, err := d.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	paneID, err := d.Tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", window.Name, "-c", dir)
	if err != nil {
		return fmt.Errorf("failed to create template window %q: %w", window.Name, err)
	}
	if _, err := d.Tmux.Command("select-pane", "-t", paneID, "-T", pane.Name); err != nil {
		return fmt.Errorf("failed to set pane title: %w", err)
	}
	if _, err := d.Tmux.Command("send-keys", "-t", paneID, pane.Command, "Enter"); err != nil {
		return fmt.Errorf("failed to send pane command: %w", err)
	}
	return nil
}

func findSessionTemplate(cfg *config.Config, name string) (config.SessionTemplate, bool) {
	if cfg == nil {
		return config.SessionTemplate{}, false
	}
	for _, tmpl := range cfg.SessionTemplates {
		if tmpl.Name == name {
			return tmpl, true
		}
	}
	return config.SessionTemplate{}, false
}

func walkingSkeletonTemplateSpec(tmpl config.SessionTemplate) (config.SessionTemplateWindow, config.SessionTemplatePaneSpec, error) {
	if tmpl.Name == "" {
		return config.SessionTemplateWindow{}, config.SessionTemplatePaneSpec{}, fmt.Errorf("name is required")
	}
	if len(tmpl.Windows) != 1 {
		return config.SessionTemplateWindow{}, config.SessionTemplatePaneSpec{}, fmt.Errorf("exactly one window is required")
	}
	window := tmpl.Windows[0]
	if window.Name == "" {
		return config.SessionTemplateWindow{}, config.SessionTemplatePaneSpec{}, fmt.Errorf("window name is required")
	}
	if window.Pane == nil {
		return config.SessionTemplateWindow{}, config.SessionTemplatePaneSpec{}, fmt.Errorf("window %q requires one pane spec", window.Name)
	}
	pane := *window.Pane
	if pane.Name == "" {
		return config.SessionTemplateWindow{}, config.SessionTemplatePaneSpec{}, fmt.Errorf("pane name is required")
	}
	if pane.Command == "" {
		return config.SessionTemplateWindow{}, config.SessionTemplatePaneSpec{}, fmt.Errorf("pane command is required")
	}
	return window, pane, nil
}
