package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/spf13/cobra"
)

type templateRuntimeDeps struct {
	Tmux        deps.Tmux
	LoadConfig  func() (*config.Config, error)
	Getwd       func() (string, error)
	UserHomeDir func() (string, error)
	ConfigDeps  *config.Deps
	ErrOut      io.Writer
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
		Getwd:       os.Getwd,
		UserHomeDir: os.UserHomeDir,
		ConfigDeps:  config.DefaultDeps(),
		ErrOut:      os.Stderr,
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
	dir, err := d.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	templates, warnings := cfg.ResolveSessionTemplatesWith(d.ConfigDeps, dir)
	for _, w := range warnings {
		warnf(d, "%s\n", w)
	}
	return runTemplateListWith(templates, cmd.OutOrStdout())
}

func runTemplateListWith(templates []config.SessionTemplate, out io.Writer) error {
	for _, tmpl := range templates {
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
	dir, err := d.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	templates, warnings := cfg.ResolveSessionTemplatesWith(d.ConfigDeps, dir)
	for _, w := range warnings {
		warnf(d, "%s\n", w)
	}
	return runTemplateApplyWith(d, templates, args[0])
}

func runTemplateApplyWith(d templateRuntimeDeps, templates []config.SessionTemplate, name string) error {
	tmpl, ok := findSessionTemplate(templates, name)
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
	homeDir, err := d.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	existing, err := sessionWindowNames(d.Tmux, session)
	if err != nil {
		return fmt.Errorf("failed to list existing windows: %w", err)
	}

	type focusTarget struct {
		windowRef string
		paneID    string
	}
	var focusTargets []focusTarget
	var firstCreatedWindowRef string
	var firstCreatedWindowLeaf string

	for _, window := range tmpl.Windows {
		if existing[window.Name] {
			warnf(d, "template window %q already exists in session %q, skipping\n", window.Name, session)
			continue
		}

		rootCwd := effectiveCwd(dir, dir, window.Pane.Cwd, homeDir)
		windowRef := session + ":" + window.Name

		// Create the window with the initial pane
		paneID, err := d.Tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", window.Name, "-c", rootCwd)
		if err != nil {
			return fmt.Errorf("failed to create template window %q: %w", window.Name, err)
		}

		// Realize the pane tree
		result, err := realizePaneTree(d.Tmux, window.Pane, paneID, dir, rootCwd, homeDir)
		if err != nil {
			return fmt.Errorf("failed to realize pane tree for window %q: %w", window.Name, err)
		}

		if firstCreatedWindowRef == "" && len(result.leafIDs) > 0 {
			firstCreatedWindowRef = windowRef
			firstCreatedWindowLeaf = result.leafIDs[0]
		}
		for _, focusID := range result.focusIDs {
			focusTargets = append(focusTargets, focusTarget{windowRef: windowRef, paneID: focusID})
		}
	}

	// Activate the requested window and pane. Default to the first created
	// window's first leaf pane when no explicit focus was requested.
	if len(focusTargets) > 1 {
		warnf(d, "multiple panes requested focus; using the first one\n")
	}
	if len(focusTargets) > 0 {
		target := focusTargets[0]
		if _, err := d.Tmux.Command("select-window", "-t", target.windowRef); err != nil {
			return fmt.Errorf("failed to select window %q: %w", target.windowRef, err)
		}
		if _, err := d.Tmux.Command("select-pane", "-t", target.paneID); err != nil {
			return fmt.Errorf("failed to select pane %q: %w", target.paneID, err)
		}
	} else if firstCreatedWindowRef != "" {
		if _, err := d.Tmux.Command("select-window", "-t", firstCreatedWindowRef); err != nil {
			return fmt.Errorf("failed to select window %q: %w", firstCreatedWindowRef, err)
		}
		if _, err := d.Tmux.Command("select-pane", "-t", firstCreatedWindowLeaf); err != nil {
			return fmt.Errorf("failed to select pane %q: %w", firstCreatedWindowLeaf, err)
		}
	}

	return nil
}

// paneTreeResult collects the leaf pane IDs created by a pane tree, plus any
// panes that requested focus.
type paneTreeResult struct {
	leafIDs  []string
	focusIDs []string
}

// realizePaneTree realizes a pane spec recursively. If the pane is a leaf,
// it sets the title and sends the command. If it's a container, it creates
// child panes via splits and recursively realizes them. parentCwd is the
// already-resolved effective working directory inherited from ancestors.
func realizePaneTree(tmux deps.Tmux, pane *config.SessionTemplatePaneSpec, paneID, sessionDir, parentCwd, homeDir string) (paneTreeResult, error) {
	if len(pane.Panes) == 0 {
		// Leaf node: set title and send command
		if _, err := tmux.Command("select-pane", "-t", paneID, "-T", pane.Name); err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to set pane title %q: %w", pane.Name, err)
		}
		if _, err := tmux.Command("send-keys", "-t", paneID, pane.Command, "Enter"); err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to send pane command %q: %w", pane.Command, err)
		}
		result := paneTreeResult{leafIDs: []string{paneID}}
		if pane.Focus {
			result.focusIDs = append(result.focusIDs, paneID)
		}
		return result, nil
	}

	// Container node: create child panes and realize them
	return realizeContainer(tmux, paneID, pane.Panes, pane.Direction, sessionDir, parentCwd, homeDir)
}

// realizeContainer creates child panes for a container and realizes them recursively.
// It splits the container pane N-1 times to create N child panes, then resizes
// them according to their weights. parentCwd is the already-resolved effective
// working directory for this container.
func realizeContainer(tmux deps.Tmux, containerPaneID string, children []config.SessionTemplatePaneSpec, direction, sessionDir, parentCwd, homeDir string) (paneTreeResult, error) {
	n := len(children)
	if n == 0 {
		return paneTreeResult{}, nil
	}
	if n == 1 {
		// Single child: reuse the container pane
		childCwd := effectiveCwd(sessionDir, parentCwd, children[0].Cwd, homeDir)
		if children[0].Cwd != "" {
			if _, err := tmux.Command("respawn-pane", "-c", childCwd, "-t", containerPaneID, "-k"); err != nil {
				return paneTreeResult{}, fmt.Errorf("failed to set pane directory to %q: %w", childCwd, err)
			}
		}
		return realizePaneTree(tmux, &children[0], containerPaneID, sessionDir, childCwd, homeDir)
	}

	// If the first child overrides the container's cwd, the container pane
	// (which was created with parentCwd) must be respawned in the child's cwd
	// before it is reused.
	child0Cwd := effectiveCwd(sessionDir, parentCwd, children[0].Cwd, homeDir)
	if children[0].Cwd != "" {
		if _, err := tmux.Command("respawn-pane", "-c", child0Cwd, "-t", containerPaneID, "-k"); err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to set pane directory to %q: %w", child0Cwd, err)
		}
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
		childCwd := effectiveCwd(sessionDir, parentCwd, children[i].Cwd, homeDir)

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

		newPaneID, err := tmux.Command("split-window", splitFlag, "-t", lastPaneID, "-p", fmt.Sprintf("%d", percentage), "-P", "-F", "#{pane_id}", "-c", childCwd)
		if err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to split pane: %w", err)
		}
		paneIDs = append(paneIDs, newPaneID)
	}

	// Resize panes to exact sizes based on weights
	if err := resizePanesByWeight(tmux, paneIDs, children, direction); err != nil {
		return paneTreeResult{}, fmt.Errorf("failed to resize panes: %w", err)
	}

	// Recursively realize child panes
	var combined paneTreeResult
	for i := range children {
		childCwd := effectiveCwd(sessionDir, parentCwd, children[i].Cwd, homeDir)
		result, err := realizePaneTree(tmux, &children[i], paneIDs[i], sessionDir, childCwd, homeDir)
		if err != nil {
			return paneTreeResult{}, err
		}
		combined.leafIDs = append(combined.leafIDs, result.leafIDs...)
		combined.focusIDs = append(combined.focusIDs, result.focusIDs...)
	}

	return combined, nil
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

func findSessionTemplate(templates []config.SessionTemplate, name string) (config.SessionTemplate, bool) {
	for _, tmpl := range templates {
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

// effectiveCwd returns the working directory for a pane given its raw cwd
// value and the inherited parent cwd. Relative paths are resolved under the
// session directory; paths starting with ~/ expand to the home directory;
// absolute paths are used as-is.
func effectiveCwd(sessionDir, parentCwd, rawCwd, homeDir string) string {
	if rawCwd == "" {
		return parentCwd
	}
	return resolveCwd(sessionDir, rawCwd, homeDir)
}

// resolveCwd resolves a non-empty cwd value relative to the session directory,
// expanding ~/ to the home directory and preserving absolute paths.
func resolveCwd(sessionDir, rawCwd, homeDir string) string {
	if strings.HasPrefix(rawCwd, "~/") {
		return filepath.Join(homeDir, rawCwd[2:])
	}
	if filepath.IsAbs(rawCwd) {
		return rawCwd
	}
	return filepath.Join(sessionDir, rawCwd)
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
