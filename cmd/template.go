package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/layout"
	"github.com/spf13/cobra"
)

type templateRuntimeDeps struct {
	Tmux        tmuxmod.Tmux
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
		Tmux: defaultTmuxMod,
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
	templates, warnings := cfg.ResolveWorkbenchesWith(d.ConfigDeps, dir)
	for _, w := range warnings {
		warnf(d, "%s\n", w)
	}
	return runTemplateListWith(templates, cmd.OutOrStdout())
}

func runTemplateListWith(templates []config.Workbench, out io.Writer) error {
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
	templates, warnings := cfg.ResolveWorkbenchesWith(d.ConfigDeps, dir)
	for _, w := range warnings {
		warnf(d, "%s\n", w)
	}
	return runTemplateApplyWith(d, templates, args[0])
}

func runTemplateApplyWith(d templateRuntimeDeps, templates []config.Workbench, name string) error {
	tmpl, ok := findWorkbench(templates, name)
	if !ok {
		return fmt.Errorf("session template %q not found", name)
	}
	if err := validateWorkbench(tmpl); err != nil {
		return fmt.Errorf("session template %q: %w", name, err)
	}

	session, err := d.Tmux.CurrentSession()
	if err != nil || session == "" {
		return fmt.Errorf("not inside a tmux session")
	}
	dir, err := d.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	return applyWorkbench(d, tmpl, session, dir)
}

// applyWorkbench realizes a validated Workbench into the existing tmux session
// `session`, with cwd = `dir`. It runs before_apply, then merges each window into
// a live pop-owned match or creates it fresh, and finally focuses. It is the
// shared core of both entry points (ADR-0075): the `workbench apply` command
// (into the current session) and the picker create-path (into a session just
// born for the Workbench).
func applyWorkbench(d templateRuntimeDeps, tmpl config.Workbench, session, dir string) error {
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
	liveWindows, err := d.Tmux.LiveWorkbenchWindows(session)
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
			liveNames, fallbackAnchor, err := d.Tmux.LivePaneIdentities(liveRef)
			if err != nil {
				return fmt.Errorf("failed to inspect live window %q: %w", window.Name, err)
			}
			merged, err := mergeWindow(d.Tmux, window.Layout, liveNames, fallbackAnchor, dir, rootCwd, homeDir, window.Name)
			if err != nil {
				return fmt.Errorf("failed to merge workbench window %q: %w", window.Name, err)
			}
			leafIDs, focusIDs = merged.leafIDs, merged.focusIDs
		} else {
			// No live match: create the window fresh.
			windowRef = session + ":" + window.Name

			// Create the window with the initial pane
			paneID, err := d.Tmux.NewWindow(session, window.Name, rootCwd)
			if err != nil {
				return fmt.Errorf("failed to create template window %q: %w", window.Name, err)
			}

			// Stamp pop-owned window identity (ADR-0075): record the spec name in a
			// user option that survives auto-rename, and disable auto-rename so the
			// display name stays stable for humans.
			if err := d.Tmux.StampWorkbenchWindow(windowRef, window.Name); err != nil {
				return fmt.Errorf("failed to stamp window identity for %q: %w", window.Name, err)
			}
			if err := d.Tmux.DisableAutomaticRename(windowRef); err != nil {
				return fmt.Errorf("failed to disable automatic-rename for window %q: %w", window.Name, err)
			}

			// Realize the pane tree
			result, err := realizePaneTree(d.Tmux, window.Layout, paneID, dir, rootCwd, homeDir, window.Name, "layout")
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
		if err := d.Tmux.SelectWindowTarget(target.windowRef); err != nil {
			return fmt.Errorf("failed to select window %q: %w", target.windowRef, err)
		}
		if err := d.Tmux.SelectPane(target.paneID); err != nil {
			return fmt.Errorf("failed to select pane %q: %w", target.paneID, err)
		}
	} else if firstWindowRef != "" {
		if err := d.Tmux.SelectWindowTarget(firstWindowRef); err != nil {
			return fmt.Errorf("failed to select window %q: %w", firstWindowRef, err)
		}
		if err := d.Tmux.SelectPane(firstWindowLeaf); err != nil {
			return fmt.Errorf("failed to select pane %q: %w", firstWindowLeaf, err)
		}
	}

	return nil
}

// createSessionFromWorkbench creates a brand-new detached tmux session named
// sessionName at path, realizes the Workbench into it, and removes the stray
// shell window that `tmux new-session` always births so the session is *exactly*
// the Workbench (ADR-0075). It does not attach — the caller switches/attaches
// afterward. The Workbench must already be validated by the caller.
func createSessionFromWorkbench(d templateRuntimeDeps, tmpl config.Workbench, sessionName, path string) error {
	if err := validateWorkbench(tmpl); err != nil {
		return fmt.Errorf("workbench %q: %w", tmpl.Name, err)
	}

	// Create the detached session and capture its initial (stray) window id.
	// Every Workbench window is created fresh — the stray window carries no
	// pop-owned window identity, so the merge walk never matches it — leaving
	// the stray to be killed once the Workbench is realized.
	strayWindow, err := d.Tmux.NewScaffoldSession(sessionName, path)
	if err != nil {
		return err
	}

	if err := applyWorkbench(d, tmpl, sessionName, path); err != nil {
		return err
	}

	if err := d.Tmux.KillWindow(strayWindow); err != nil {
		return fmt.Errorf("failed to remove stray shell window: %w", err)
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
func mergeWindow(tmux tmuxmod.Tmux, layout *config.WorkbenchPaneSpec, liveNames map[string]string, fallbackAnchor, sessionDir, rootCwd, homeDir, windowName string) (mergeResult, error) {
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
		realized, err := realizePaneTree(tmux, layout, fallbackAnchor, sessionDir, rootCwd, homeDir, windowName, "layout")
		if err != nil {
			return mergeResult{}, err
		}
		return mergeResult{anchor: fallbackAnchor, leafIDs: realized.leafIDs, focusIDs: realized.focusIDs}, nil
	}
	return mergeContainer(tmux, layout, liveNames, fallbackAnchor, sessionDir, rootCwd, homeDir, windowName, "layout")
}

// mergePaneTree merges a present-live subtree. A matched leaf is left untouched
// (its process survives); a present container recurses. Wholly-missing subtrees
// are never routed here — their container parent builds them fresh.
func mergePaneTree(tmux tmuxmod.Tmux, pane *config.WorkbenchPaneSpec, liveNames map[string]string, sessionDir, parentCwd, homeDir, windowName, specPath string) (mergeResult, error) {
	if len(pane.Panes) == 0 {
		id := liveNames[pane.Name]
		res := mergeResult{anchor: id, leafIDs: []string{id}}
		if pane.Focus {
			res.focusIDs = append(res.focusIDs, id)
		}
		return res, nil
	}
	return mergeContainer(tmux, pane, liveNames, "", sessionDir, parentCwd, homeDir, windowName, specPath)
}

// mergeContainer walks a container's children in target order. Present children
// are merged (recursing into live panes); missing children are appended by
// splitting off the nearest live sibling — forward off the last placed sibling,
// or before the next live sibling when none precedes. After placement the
// container is reproportioned to the target weights, which may resize (never
// kill) surviving panes. fallbackAnchor seeds the split when the container has no
// live children at all (only reachable at a matched window root).
func mergeContainer(tmux tmuxmod.Tmux, container *config.WorkbenchPaneSpec, liveNames map[string]string, fallbackAnchor, sessionDir, parentCwd, homeDir, windowName, specPath string) (mergeResult, error) {
	children := container.Panes
	direction := container.Children
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

	if n > 1 {
		measureTarget := fallbackAnchor
		if measureTarget == "" {
			for i := range children {
				if present[i] {
					if id := firstLiveLeaf(&children[i], liveNames); id != "" {
						measureTarget = id
						break
					}
				}
			}
		}
		if measureTarget != "" {
			if err := checkContainerFits(tmux, measureTarget, direction, n, windowName, specPath, container); err != nil {
				return mergeResult{}, err
			}
		}
	}

	childAnchors := make([]string, n)
	var combined mergeResult
	lastAnchor := ""

	for i := range children {
		childCwd := effectiveCwd(sessionDir, parentCwd, children[i].Cwd, homeDir)
		childPath := fmt.Sprintf("%s.panes[%d]", specPath, i)

		if present[i] {
			merged, err := mergePaneTree(tmux, &children[i], liveNames, sessionDir, childCwd, homeDir, windowName, childPath)
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
		spec := tmuxmod.SplitSpec{Horizontal: splitFlag == "-h", Dir: childCwd}
		if lastAnchor != "" {
			spec.Target = lastAnchor
		} else {
			anchor := nextLiveAnchor(children, present, liveNames, i)
			if anchor == "" {
				anchor = fallbackAnchor
			}
			spec.Target = anchor
			spec.Before = true
		}
		newPaneID, err := tmux.SplitPane(spec)
		if err != nil {
			return mergeResult{}, fmt.Errorf("failed to split for pane %q: %w", children[i].Name, err)
		}
		realized, err := realizePaneTree(tmux, &children[i], newPaneID, sessionDir, childCwd, homeDir, windowName, childPath)
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
func subtreeLive(pane *config.WorkbenchPaneSpec, liveNames map[string]string) bool {
	return firstLiveLeaf(pane, liveNames) != ""
}

// firstLiveLeaf returns the live pane id of the first named leaf in the subtree
// (tree order) that matches a live pane, or "" if none is present.
func firstLiveLeaf(pane *config.WorkbenchPaneSpec, liveNames map[string]string) string {
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
func nextLiveAnchor(children []config.WorkbenchPaneSpec, present []bool, liveNames map[string]string, from int) string {
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
func realizePaneTree(tmux tmuxmod.Tmux, pane *config.WorkbenchPaneSpec, paneID, sessionDir, parentCwd, homeDir, windowName, specPath string) (paneTreeResult, error) {
	if len(pane.Panes) == 0 {
		// Leaf node: set title and send command
		if err := tmux.SetPaneTitle(paneID, pane.Name); err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to set pane title %q: %w", pane.Name, err)
		}
		// Stamp pop-owned pane identity (ADR-0075/ADR-0058) on named leaves so a
		// later reapply can match this pane by identity regardless of how its
		// display title gets clobbered. Unnamed leaves are anonymous (B1) — no stamp.
		if pane.Name != "" {
			if err := tmux.StampPane(paneID, pane.Name); err != nil {
				return paneTreeResult{}, fmt.Errorf("failed to stamp pane identity %q: %w", pane.Name, err)
			}
		}
		if err := tmux.SendKeys(paneID, pane.Command, "Enter"); err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to send pane command %q: %w", pane.Command, err)
		}
		result := paneTreeResult{leafIDs: []string{paneID}}
		if pane.Focus {
			result.focusIDs = append(result.focusIDs, paneID)
		}
		return result, nil
	}

	// Container node: create child panes and realize them
	return realizeContainer(tmux, paneID, pane, sessionDir, parentCwd, homeDir, windowName, specPath)
}

// realizeContainer creates child panes for a container and realizes them recursively.
// It splits the container pane N-1 times to create N child panes, then corrects
// them to the same apportioned cell counts the splits carried. parentCwd is the
// already-resolved effective working directory for this container.
func realizeContainer(tmux tmuxmod.Tmux, containerPaneID string, container *config.WorkbenchPaneSpec, sessionDir, parentCwd, homeDir, windowName, specPath string) (paneTreeResult, error) {
	children := container.Panes
	direction := container.Children
	n := len(children)
	if n == 0 {
		return paneTreeResult{}, nil
	}
	if n == 1 {
		// Single child: reuse the container pane
		childCwd := effectiveCwd(sessionDir, parentCwd, children[0].Cwd, homeDir)
		if children[0].Cwd != "" {
			if err := tmux.RespawnPane(containerPaneID, childCwd); err != nil {
				return paneTreeResult{}, fmt.Errorf("failed to set pane directory to %q: %w", childCwd, err)
			}
		}
		childPath := fmt.Sprintf("%s.panes[0]", specPath)
		return realizePaneTree(tmux, &children[0], containerPaneID, sessionDir, childCwd, homeDir, windowName, childPath)
	}

	// If the first child overrides the container's cwd, the container pane
	// (which was created with parentCwd) must be respawned in the child's cwd
	// before it is reused.
	child0Cwd := effectiveCwd(sessionDir, parentCwd, children[0].Cwd, homeDir)
	if children[0].Cwd != "" {
		if err := tmux.RespawnPane(containerPaneID, child0Cwd); err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to set pane directory to %q: %w", child0Cwd, err)
		}
	}

	// Read this container's own pane extent before any split — correct at any
	// nesting depth, including same-direction nesting (ADR-0159).
	width, height, err := tmux.PaneSize(containerPaneID)
	if err != nil {
		return paneTreeResult{}, fmt.Errorf("failed to get pane size: %w", err)
	}
	horizontal := direction == "columns"
	extent := height
	if horizontal {
		extent = width
	}

	if err := checkContainerFitsExtent(extent, direction, n, windowName, specPath, container); err != nil {
		return paneTreeResult{}, err
	}

	weights := make([]int, n)
	for i, child := range children {
		weights[i] = child.Weight
	}
	cells := layout.Apportion(extent, weights)

	// Determine split flag based on children orientation
	splitFlag := "-h" // columns = side-by-side
	if direction == "rows" {
		splitFlag = "-v" // rows = stacked
	}

	// Create panes by splitting. Each split cuts the last pane made; the new
	// pane is the remaining tail (children i..n-1 plus their future borders),
	// sized with -l so the surviving pane keeps cells[i-1].
	paneIDs := []string{containerPaneID}
	for i := 1; i < n; i++ {
		lastPaneID := paneIDs[len(paneIDs)-1]
		childCwd := effectiveCwd(sessionDir, parentCwd, children[i].Cwd, homeDir)

		newPaneID, err := tmux.SplitPane(tmuxmod.SplitSpec{
			Target:     lastPaneID,
			Horizontal: splitFlag == "-h",
			Cells:      remainingSplitCells(cells, i),
			Dir:        childCwd,
		})
		if err != nil {
			return paneTreeResult{}, fmt.Errorf("failed to split pane: %w", err)
		}
		paneIDs = append(paneIDs, newPaneID)
	}

	// Correction pass: repair whatever tmux clamped, using the same cells.
	if err := resizePanesToCells(tmux, paneIDs, cells, horizontal); err != nil {
		return paneTreeResult{}, fmt.Errorf("failed to resize panes: %w", err)
	}

	// Recursively realize child panes
	var combined paneTreeResult
	for i := range children {
		childCwd := effectiveCwd(sessionDir, parentCwd, children[i].Cwd, homeDir)
		childPath := fmt.Sprintf("%s.panes[%d]", specPath, i)
		result, err := realizePaneTree(tmux, &children[i], paneIDs[i], sessionDir, childCwd, homeDir, windowName, childPath)
		if err != nil {
			return paneTreeResult{}, err
		}
		combined.leafIDs = append(combined.leafIDs, result.leafIDs...)
		combined.focusIDs = append(combined.focusIDs, result.focusIDs...)
	}

	return combined, nil
}

// remainingSplitCells is the -l size for the new pane when splitting to peel
// child i-1 off the tail. The new pane holds children i..n-1, so it needs their
// cell counts plus the n-i-1 borders those further splits will consume.
func remainingSplitCells(cells []int, i int) int {
	n := len(cells)
	sum := 0
	for j := i; j < n; j++ {
		sum += cells[j]
	}
	return sum + (n - i - 1)
}

// resizePanesByWeight apportions child weights against the first pane's current
// extent and resizes every pane to those counts. Used by merge reproportion,
// where the container pane no longer exists as a single pane.
func resizePanesByWeight(tmux tmuxmod.Tmux, paneIDs []string, children []config.WorkbenchPaneSpec, direction string) error {
	target := paneIDs[0]

	width, height, err := tmux.PaneSize(target)
	if err != nil {
		return fmt.Errorf("failed to get pane size: %w", err)
	}

	horizontal := direction == "columns"
	extent := height
	if horizontal {
		extent = width
	}

	weights := make([]int, len(children))
	for i, child := range children {
		weights[i] = child.Weight
	}
	return resizePanesToCells(tmux, paneIDs, layout.Apportion(extent, weights), horizontal)
}

// resizePanesToCells resizes each pane to the given cell count along the
// container's split axis.
func resizePanesToCells(tmux tmuxmod.Tmux, paneIDs []string, cells []int, horizontal bool) error {
	for i, paneID := range paneIDs {
		if err := tmux.ResizePane(paneID, horizontal, cells[i]); err != nil {
			return fmt.Errorf("failed to resize pane %s: %w", paneID, err)
		}
	}
	return nil
}

func checkContainerFits(tmux tmuxmod.Tmux, measureTarget, direction string, childCount int, windowName, specPath string, container *config.WorkbenchPaneSpec) error {
	width, height, err := tmux.PaneSize(measureTarget)
	if err != nil {
		return fmt.Errorf("failed to get pane size: %w", err)
	}
	extent := height
	if direction == "columns" {
		extent = width
	}
	return checkContainerFitsExtent(extent, direction, childCount, windowName, specPath, container)
}

func checkContainerFitsExtent(extent int, direction string, childCount int, windowName, specPath string, container *config.WorkbenchPaneSpec) error {
	if childCount <= 1 {
		return nil
	}
	budget := layout.CellBudget(extent, childCount)
	if layout.FitsMinCells(budget, childCount, layout.MinPaneCells) {
		return nil
	}
	axis := "rows"
	if direction == "columns" {
		axis = "columns"
	}
	return fmt.Errorf(
		"window %q: pane spec %s cannot fit %d %s in %d cells (cell budget %d, need %d)",
		windowName,
		formatPaneSpecRef(specPath, container),
		childCount,
		axis,
		extent,
		budget,
		childCount*layout.MinPaneCells,
	)
}

func formatPaneSpecRef(specPath string, pane *config.WorkbenchPaneSpec) string {
	if pane != nil && pane.Name != "" {
		return fmt.Sprintf("%q", pane.Name)
	}
	if specPath == "" {
		return "layout"
	}
	return specPath
}

func findWorkbench(templates []config.Workbench, name string) (config.Workbench, bool) {
	for _, tmpl := range templates {
		if tmpl.Name == name {
			return tmpl, true
		}
	}
	return config.Workbench{}, false
}

func validateWorkbench(tmpl config.Workbench) error {
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
func validatePaneSpec(pane *config.WorkbenchPaneSpec, path string) error {
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
