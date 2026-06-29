package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/spf13/cobra"
)

type templateRuntimeDeps struct {
	Tmux       deps.Tmux
	LoadConfig func() (*config.Config, error)
	Getwd      func() (string, error)
	ErrOut     io.Writer
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
		Getwd:  os.Getwd,
		ErrOut: os.Stderr,
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
	if err := validateSessionTemplate(tmpl); err != nil {
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

	existing, err := sessionWindowNames(d.Tmux, session)
	if err != nil {
		return fmt.Errorf("failed to list existing windows: %w", err)
	}

	for _, window := range tmpl.Windows {
		if existing[window.Name] {
			warnf(d, "template window %q already exists in session %q, skipping\n", window.Name, session)
			continue
		}

		// Create the window with the initial pane
		paneID, err := d.Tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", window.Name, "-c", dir)
		if err != nil {
			return fmt.Errorf("failed to create template window %q: %w", window.Name, err)
		}

		// Realize the pane tree
		if err := realizePaneTree(d.Tmux, window.Pane, paneID); err != nil {
			return fmt.Errorf("failed to realize pane tree for window %q: %w", window.Name, err)
		}
	}

	return nil
}

// realizePaneTree realizes a pane spec recursively. If the pane is a leaf,
// it sets the title and sends the command. If it's a container, it creates
// child panes via splits and recursively realizes them.
func realizePaneTree(tmux deps.Tmux, pane *config.SessionTemplatePaneSpec, paneID string) error {
	if len(pane.Panes) == 0 {
		// Leaf node: set title and send command
		if _, err := tmux.Command("select-pane", "-t", paneID, "-T", pane.Name); err != nil {
			return fmt.Errorf("failed to set pane title %q: %w", pane.Name, err)
		}
		if _, err := tmux.Command("send-keys", "-t", paneID, pane.Command, "Enter"); err != nil {
			return fmt.Errorf("failed to send pane command %q: %w", pane.Command, err)
		}
		return nil
	}

	// Container node: create child panes and realize them
	if err := realizeContainer(tmux, paneID, pane.Panes, pane.Direction); err != nil {
		return err
	}
	return nil
}

// realizeContainer creates child panes for a container and realizes them recursively.
// It splits the container pane N-1 times to create N child panes, then resizes
// them according to their weights.
func realizeContainer(tmux deps.Tmux, containerPaneID string, children []config.SessionTemplatePaneSpec, direction string) error {
	n := len(children)
	if n == 0 {
		return nil
	}
	if n == 1 {
		// Single child: reuse the container pane
		return realizePaneTree(tmux, &children[0], containerPaneID)
	}

	// Calculate total weight
	totalWeight := 0
	for _, child := range children {
		weight := child.Weight
		if weight == 0 {
			weight = 1
		}
		totalWeight += weight
	}

	// Determine split flag based on direction
	splitFlag := "-h" // row = side-by-side
	if direction == "column" {
		splitFlag = "-v" // column = stacked
	}

	// Create panes by splitting
	paneIDs := []string{containerPaneID}
	for i := 1; i < n; i++ {
		// Split the last pane to create a new one
		lastPaneID := paneIDs[len(paneIDs)-1]
		
		// Calculate percentage for the new pane
		// The new pane should get: (weight[i] + ... + weight[n-1]) / (weight[i-1] + ... + weight[n-1])
		remainingWeight := 0
		for j := i; j < n; j++ {
			weight := children[j].Weight
			if weight == 0 {
				weight = 1
			}
			remainingWeight += weight
		}
		previousRemaining := remainingWeight
		weightPrev := children[i-1].Weight
		if weightPrev == 0 {
			weightPrev = 1
		}
		previousRemaining += weightPrev
		
		percentage := (remainingWeight * 100) / previousRemaining
		
		newPaneID, err := tmux.Command("split-window", splitFlag, "-t", lastPaneID, "-p", fmt.Sprintf("%d", percentage), "-P", "-F", "#{pane_id}", "-c", "#{pane_current_path}")
		if err != nil {
			return fmt.Errorf("failed to split pane: %w", err)
		}
		paneIDs = append(paneIDs, newPaneID)
	}

	// Resize panes to exact sizes based on weights
	if err := resizePanesByWeight(tmux, paneIDs, children, direction); err != nil {
		return fmt.Errorf("failed to resize panes: %w", err)
	}

	// Recursively realize child panes
	for i := range children {
		if err := realizePaneTree(tmux, &children[i], paneIDs[i]); err != nil {
			return err
		}
	}

	return nil
}

// resizePanesByWeight resizes panes to match their weights. It queries the
// window dimensions and calculates target sizes in cells.
func resizePanesByWeight(tmux deps.Tmux, paneIDs []string, children []config.SessionTemplatePaneSpec, direction string) error {
	// Get window dimensions
	widthStr, err := tmux.Command("display-message", "-p", "#{window_width}")
	if err != nil {
		return fmt.Errorf("failed to get window width: %w", err)
	}
	heightStr, err := tmux.Command("display-message", "-p", "#{window_height}")
	if err != nil {
		return fmt.Errorf("failed to get window height: %w", err)
	}

	var width, height int
	fmt.Sscanf(widthStr, "%d", &width)
	fmt.Sscanf(heightStr, "%d", &height)

	// Calculate total weight
	totalWeight := 0
	for _, child := range children {
		weight := child.Weight
		if weight == 0 {
			weight = 1
		}
		totalWeight += weight
	}

	// Determine which dimension to resize
	var totalSize int
	var resizeFlag string
	if direction == "row" {
		totalSize = width
		resizeFlag = "-x"
	} else {
		totalSize = height
		resizeFlag = "-y"
	}

	// Resize each pane to its target size
	for i, paneID := range paneIDs {
		weight := children[i].Weight
		if weight == 0 {
			weight = 1
		}
		targetSize := (totalSize * weight) / totalWeight
		
		_, err := tmux.Command("resize-pane", "-t", paneID, resizeFlag, fmt.Sprintf("%d", targetSize))
		if err != nil {
			return fmt.Errorf("failed to resize pane %s: %w", paneID, err)
		}
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

func validateSessionTemplate(tmpl config.SessionTemplate) error {
	if tmpl.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(tmpl.Windows) == 0 {
		return fmt.Errorf("at least one window is required")
	}
	for i, window := range tmpl.Windows {
		if window.Name == "" {
			return fmt.Errorf("window[%d] name is required", i)
		}
		if window.Pane == nil {
			return fmt.Errorf("window %q requires a pane spec", window.Name)
		}
		if err := validatePaneSpec(window.Pane, ""); err != nil {
			return err
		}
	}
	return nil
}

// sessionWindowNames returns the set of window names in the given tmux session.
func sessionWindowNames(tmux deps.Tmux, session string) (map[string]bool, error) {
	out, err := tmux.Command("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			names[line] = true
		}
	}
	return names, nil
}

func warnf(d templateRuntimeDeps, format string, args ...any) {
	if d.ErrOut != nil {
		fmt.Fprintf(d.ErrOut, format, args...)
	} else {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

// validatePaneSpec validates a pane spec recursively. A pane is either a leaf
// (has name and command, no panes) or a container (has direction and panes).
func validatePaneSpec(pane *config.SessionTemplatePaneSpec, path string) error {
	isContainer := len(pane.Panes) > 0
	
	if isContainer {
		// Container node
		if pane.Direction != "row" && pane.Direction != "column" {
			return fmt.Errorf("%spane must have direction 'row' or 'column' when it has nested panes", path)
		}
		// Recursively validate children
		for i := range pane.Panes {
			childPath := fmt.Sprintf("%spanes[%d].", path, i)
			if err := validatePaneSpec(&pane.Panes[i], childPath); err != nil {
				return err
			}
		}
	} else {
		// Leaf node
		if pane.Name == "" {
			return fmt.Errorf("%spane name is required", path)
		}
		if pane.Command == "" {
			return fmt.Errorf("%spane command is required", path)
		}
	}
	return nil
}
