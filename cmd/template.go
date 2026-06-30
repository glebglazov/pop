package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
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
	// RunBeforeApply runs one before_apply shell command with cwd = dir
	// (the session directory). Injected so tests can observe ordering and cwd.
	RunBeforeApply func(command, dir string) error
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
		Getwd:          os.Getwd,
		UserHomeDir:    os.UserHomeDir,
		ConfigDeps:     config.DefaultDeps(),
		ErrOut:         os.Stderr,
		RunBeforeApply: runBeforeApplyCommand,
	}
}

// runBeforeApplyCommand runs a single before_apply shell command synchronously
// with cwd = dir (the session directory), streaming its output to the user's
// terminal. It is the production implementation of templateRuntimeDeps.RunBeforeApply.
func runBeforeApplyCommand(command, dir string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

var workbenchCmd = &cobra.Command{
	Use:     "workbench",
	Aliases: []string{"wb"},
	Short:   "Manage workbenches",
}

var workbenchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workbenches",
	Args:  cobra.NoArgs,
	RunE:  runTemplateList,
}

var workbenchApplyCmd = &cobra.Command{
	Use:   "apply <name>",
	Short: "Apply a workbench to the current tmux session",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateApply,
}

// layoutCmd is a hidden backward-compat alias; use workbench instead.
var layoutCmd = &cobra.Command{
	Use:    "layout",
	Short:  "Manage workbenches (deprecated: use workbench)",
	Hidden: true,
}

var layoutListCmd = &cobra.Command{
	Use:    "list",
	Short:  "List workbenches",
	Args:   cobra.NoArgs,
	RunE:   runTemplateList,
	Hidden: true,
}

var layoutApplyCmd = &cobra.Command{
	Use:    "apply <name>",
	Short:  "Apply a workbench to the current tmux session",
	Args:   cobra.ExactArgs(1),
	RunE:   runTemplateApply,
	Hidden: true,
}

func init() {
	rootCmd.AddCommand(workbenchCmd)
	workbenchCmd.AddCommand(workbenchListCmd)
	workbenchCmd.AddCommand(workbenchApplyCmd)

	rootCmd.AddCommand(layoutCmd)
	layoutCmd.AddCommand(layoutListCmd)
	layoutCmd.AddCommand(layoutApplyCmd)
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

	// Run the Workbench's before_apply commands for one-time side effects
	// (repo setup) before any window is realized, with cwd = the session
	// directory (ADR-0075). They run on every apply, including a reapply over a
	// live session; the Workbench author owns idempotency.
	for i, command := range tmpl.BeforeApply {
		if d.RunBeforeApply == nil {
			break
		}
		if err := d.RunBeforeApply(command, dir); err != nil {
			return fmt.Errorf("before_apply[%d] %q failed: %w", i, command, err)
		}
	}

	// Match target windows to live windows by pop-owned identity (ADR-0075),
	// never by the clobberable window_name. A matched window is merged into; an
	// unmatched one is created fresh.
	liveWindows, err := liveWorkbenchWindows(d.Tmux, session)
	if err != nil {
		return fmt.Errorf("failed to list existing windows: %w", err)
	}

	type focusTarget struct {
		windowRef string
		paneID    string
	}
	var focusTargets []focusTarget
	var firstWindowRef string
	var firstWindowLeaf string

	for _, window := range tmpl.Windows {
		rootCwd := effectiveCwd(dir, dir, window.Layout.Cwd, homeDir)

		var windowRef string
		var leafIDs, focusIDs []string

		if liveRef, ok := liveWindows[window.Name]; ok {
			// Matched a live pop-owned window: recurse and merge instead of
			// skipping (ADR-0075), growing it without killing running panes.
			windowRef = liveRef
			liveNames, fallbackAnchor, err := livePaneIdentities(d.Tmux, liveRef)
			if err != nil {
				return fmt.Errorf("failed to inspect live window %q: %w", window.Name, err)
			}
			merged, err := mergeWindow(d.Tmux, window.Layout, liveNames, fallbackAnchor, dir, rootCwd, homeDir)
			if err != nil {
				return fmt.Errorf("failed to merge workbench window %q: %w", window.Name, err)
			}
			leafIDs, focusIDs = merged.leafIDs, merged.focusIDs
		} else {
			// No live match: create the window fresh.
			windowRef = session + ":" + window.Name

			// Create the window with the initial pane
			paneID, err := d.Tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session+":", "-n", window.Name, "-c", rootCwd)
			if err != nil {
				return fmt.Errorf("failed to create template window %q: %w", window.Name, err)
			}

			// Stamp pop-owned window identity (ADR-0075): record the spec name in a
			// user option that survives auto-rename, and disable auto-rename so the
			// display name stays stable for humans.
			if _, err := d.Tmux.Command("set-option", "-w", "-t", windowRef, "@pop_wb_window", window.Name); err != nil {
				return fmt.Errorf("failed to stamp window identity for %q: %w", window.Name, err)
			}
			if _, err := d.Tmux.Command("set-option", "-w", "-t", windowRef, "automatic-rename", "off"); err != nil {
				return fmt.Errorf("failed to disable automatic-rename for window %q: %w", window.Name, err)
			}

			// Realize the pane tree
			result, err := realizePaneTree(d.Tmux, window.Layout, paneID, dir, rootCwd, homeDir)
			if err != nil {
				return fmt.Errorf("failed to realize pane tree for window %q: %w", window.Name, err)
			}
			leafIDs, focusIDs = result.leafIDs, result.focusIDs
		}

		if firstWindowRef == "" && len(leafIDs) > 0 {
			firstWindowRef = windowRef
			firstWindowLeaf = leafIDs[0]
		}
		for _, focusID := range focusIDs {
			focusTargets = append(focusTargets, focusTarget{windowRef: windowRef, paneID: focusID})
		}
	}

	// Activate the requested window and pane. Default to the first window's
	// first leaf pane when no explicit focus was requested.
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
	} else if firstWindowRef != "" {
		if _, err := d.Tmux.Command("select-window", "-t", firstWindowRef); err != nil {
			return fmt.Errorf("failed to select window %q: %w", firstWindowRef, err)
		}
		if _, err := d.Tmux.Command("select-pane", "-t", firstWindowLeaf); err != nil {
			return fmt.Errorf("failed to select pane %q: %w", firstWindowLeaf, err)
		}
	}

	return nil
}

// mergeResult collects the leaf pane IDs touched by a merge walk (live survivors
// plus newly appended panes), any panes requesting focus, and the subtree's
// anchor pane — the representative pane a parent container splits off and resizes.
type mergeResult struct {
	anchor   string
	leafIDs  []string
	focusIDs []string
}

// mergeWindow merges a target window's layout into a live, pop-owned window
// (ADR-0075). It is the entry point that decides whether the window root is a
// single leaf or a container.
func mergeWindow(tmux deps.Tmux, layout *config.SessionTemplatePaneSpec, liveNames map[string]string, fallbackAnchor, sessionDir, rootCwd, homeDir string) (mergeResult, error) {
	if len(layout.Panes) == 0 {
		if id := liveNames[layout.Name]; id != "" {
			// The sole pane survived: leave its process intact.
			res := mergeResult{anchor: id, leafIDs: []string{id}}
			if layout.Focus {
				res.focusIDs = append(res.focusIDs, id)
			}
			return res, nil
		}
		// Window matched but its only named pane is gone — rebuild into the
		// window's surviving pane rather than spawning a duplicate window.
		realized, err := realizePaneTree(tmux, layout, fallbackAnchor, sessionDir, rootCwd, homeDir)
		if err != nil {
			return mergeResult{}, err
		}
		return mergeResult{anchor: fallbackAnchor, leafIDs: realized.leafIDs, focusIDs: realized.focusIDs}, nil
	}
	return mergeContainer(tmux, layout.Panes, layout.Children, liveNames, fallbackAnchor, sessionDir, rootCwd, homeDir)
}

// mergePaneTree merges a present-live subtree. A matched leaf is left untouched
// (its process survives); a present container recurses. Wholly-missing subtrees
// are never routed here — their container parent builds them fresh.
func mergePaneTree(tmux deps.Tmux, pane *config.SessionTemplatePaneSpec, liveNames map[string]string, sessionDir, parentCwd, homeDir string) (mergeResult, error) {
	if len(pane.Panes) == 0 {
		id := liveNames[pane.Name]
		res := mergeResult{anchor: id, leafIDs: []string{id}}
		if pane.Focus {
			res.focusIDs = append(res.focusIDs, id)
		}
		return res, nil
	}
	return mergeContainer(tmux, pane.Panes, pane.Children, liveNames, "", sessionDir, parentCwd, homeDir)
}

// mergeContainer walks a container's children in target order. Present children
// are merged (recursing into live panes); missing children are appended by
// splitting off the nearest live sibling — forward off the last placed sibling,
// or before the next live sibling when none precedes. After placement the
// container is reproportioned to the target weights, which may resize (never
// kill) surviving panes. fallbackAnchor seeds the split when the container has no
// live children at all (only reachable at a matched window root).
func mergeContainer(tmux deps.Tmux, children []config.SessionTemplatePaneSpec, direction string, liveNames map[string]string, fallbackAnchor, sessionDir, parentCwd, homeDir string) (mergeResult, error) {
	n := len(children)
	if n == 0 {
		return mergeResult{}, nil
	}

	splitFlag := "-h" // columns = side-by-side
	if direction == "rows" {
		splitFlag = "-v" // rows = stacked
	}

	present := make([]bool, n)
	for i := range children {
		present[i] = subtreeLive(&children[i], liveNames)
	}

	childAnchors := make([]string, n)
	var combined mergeResult
	lastAnchor := ""

	for i := range children {
		childCwd := effectiveCwd(sessionDir, parentCwd, children[i].Cwd, homeDir)

		if present[i] {
			merged, err := mergePaneTree(tmux, &children[i], liveNames, sessionDir, childCwd, homeDir)
			if err != nil {
				return mergeResult{}, err
			}
			childAnchors[i] = merged.anchor
			combined.leafIDs = append(combined.leafIDs, merged.leafIDs...)
			combined.focusIDs = append(combined.focusIDs, merged.focusIDs...)
			lastAnchor = merged.anchor
			continue
		}

		// Missing child: splice it in beside its live siblings, preserving
		// target order. Split forward off the last placed sibling; if none
		// precedes it, split before the next live sibling (-b).
		var splitArgs []string
		if lastAnchor != "" {
			splitArgs = []string{"split-window", splitFlag, "-t", lastAnchor, "-P", "-F", "#{pane_id}", "-c", childCwd}
		} else {
			anchor := nextLiveAnchor(children, present, liveNames, i)
			if anchor == "" {
				anchor = fallbackAnchor
			}
			splitArgs = []string{"split-window", splitFlag, "-b", "-t", anchor, "-P", "-F", "#{pane_id}", "-c", childCwd}
		}
		newPaneID, err := tmux.Command(splitArgs...)
		if err != nil {
			return mergeResult{}, fmt.Errorf("failed to split for pane %q: %w", children[i].Name, err)
		}
		realized, err := realizePaneTree(tmux, &children[i], newPaneID, sessionDir, childCwd, homeDir)
		if err != nil {
			return mergeResult{}, err
		}
		childAnchors[i] = newPaneID
		combined.leafIDs = append(combined.leafIDs, realized.leafIDs...)
		combined.focusIDs = append(combined.focusIDs, realized.focusIDs...)
		lastAnchor = newPaneID
	}

	// Reproportion to honor target weights. Surviving panes may shrink or grow
	// (a new sibling must take cells) but are never killed.
	if n > 1 {
		if err := resizePanesByWeight(tmux, childAnchors, children, direction); err != nil {
			return mergeResult{}, fmt.Errorf("failed to reproportion panes: %w", err)
		}
	}
	combined.anchor = childAnchors[0]
	return combined, nil
}

// subtreeLive reports whether any named leaf in the subtree matches a live pane.
func subtreeLive(pane *config.SessionTemplatePaneSpec, liveNames map[string]string) bool {
	return firstLiveLeaf(pane, liveNames) != ""
}

// firstLiveLeaf returns the live pane id of the first named leaf in the subtree
// (tree order) that matches a live pane, or "" if none is present.
func firstLiveLeaf(pane *config.SessionTemplatePaneSpec, liveNames map[string]string) string {
	if len(pane.Panes) == 0 {
		if pane.Name == "" {
			return ""
		}
		return liveNames[pane.Name]
	}
	for i := range pane.Panes {
		if id := firstLiveLeaf(&pane.Panes[i], liveNames); id != "" {
			return id
		}
	}
	return ""
}

// nextLiveAnchor returns the anchor pane of the first present sibling after
// index `from`, or "" if every following sibling is missing.
func nextLiveAnchor(children []config.SessionTemplatePaneSpec, present []bool, liveNames map[string]string, from int) string {
	for j := from + 1; j < len(children); j++ {
		if present[j] {
			return firstLiveLeaf(&children[j], liveNames)
		}
	}
	return ""
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
		// Stamp pop-owned pane identity (ADR-0075/ADR-0058) on named leaves so a
		// later reapply can match this pane via #{@pop_pane} regardless of how its
		// display title gets clobbered. Unnamed leaves are anonymous (B1) — no stamp.
		if pane.Name != "" {
			if _, err := tmux.Command("set-option", "-p", "-t", paneID, "@pop_pane", pane.Name); err != nil {
				return paneTreeResult{}, fmt.Errorf("failed to stamp pane identity %q: %w", pane.Name, err)
			}
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
	return realizeContainer(tmux, paneID, pane.Panes, pane.Children, sessionDir, parentCwd, homeDir)
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

	// Determine split flag based on children orientation
	splitFlag := "-h" // columns = side-by-side
	if direction == "rows" {
		splitFlag = "-v" // rows = stacked
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
	if direction == "columns" {
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
		if window.Layout == nil {
			return fmt.Errorf("window %q requires a layout spec", window.Name)
		}
		if err := validatePaneSpec(window.Layout, ""); err != nil {
			return err
		}
	}
	return nil
}

// liveWorkbenchWindows maps each pop-stamped window's @pop_wb_window identity to
// its tmux window id within the session. Windows lacking the stamp (anything not
// born of a Workbench apply) are skipped — identity never lives in the
// clobberable window_name (ADR-0075).
func liveWorkbenchWindows(tmux deps.Tmux, session string) (map[string]string, error) {
	out, err := tmux.Command("list-windows", "-t", session, "-F", "#{@pop_wb_window}\t#{window_id}")
	if err != nil {
		return nil, err
	}
	windows := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		if _, ok := windows[parts[0]]; !ok {
			windows[parts[0]] = parts[1]
		}
	}
	return windows, nil
}

// livePaneIdentities maps the @pop_pane identity of each stamped pane in a window
// to its tmux pane id, and returns the window's first pane id as a fallback
// anchor for the rare matched-window-with-no-recognizable-panes case.
func livePaneIdentities(tmux deps.Tmux, windowRef string) (map[string]string, string, error) {
	out, err := tmux.Command("list-panes", "-t", windowRef, "-F", "#{@pop_pane}\t#{pane_id}")
	if err != nil {
		return nil, "", err
	}
	names := make(map[string]string)
	fallback := ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if fallback == "" {
			fallback = parts[1]
		}
		if parts[0] != "" {
			if _, ok := names[parts[0]]; !ok {
				names[parts[0]] = parts[1]
			}
		}
	}
	return names, fallback, nil
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
// (has a command, no panes) or a container (has direction and panes). A leaf
// name is optional: an unnamed leaf is anonymous and always (re)created on
// reapply (ADR-0075 B1), trading reapply-safety for not having to name it.
func validatePaneSpec(pane *config.SessionTemplatePaneSpec, path string) error {
	isContainer := len(pane.Panes) > 0

	if isContainer {
		// Container node
		if pane.Children != "rows" && pane.Children != "columns" {
			return fmt.Errorf("%spane must have children 'rows' or 'columns' when it has nested panes", path)
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
		if pane.Command == "" {
			return fmt.Errorf("%spane command is required", path)
		}
	}
	return nil
}
