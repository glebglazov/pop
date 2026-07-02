package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
)

// preferredResolverConfigDeps returns config.Deps with the ADR-0078 trunk
// inheritance layer wired: a worktree with no preference of its own inherits
// the Trunk worktree's runtime entry, resolved dynamically at open. The trunk
// is the existing Trunk worktree resolution (non-bare git main worktree, or
// bare trunk = true via binding.ResolveTrunkPath); a bare repo with no trunk
// anchor yields ("", false) so the inheritance layer is skipped.
func preferredResolverConfigDeps(cfg *config.Config) *config.Deps {
	d := config.DefaultDeps()
	d.Trunk = func(checkoutPath string) (string, bool) {
		trunkPath, bare, err := binding.ResolveTrunkPath(tasks.DefaultDeps(), cfg, checkoutPath)
		if err != nil || bare || strings.TrimSpace(trunkPath) == "" {
			return "", false
		}
		return trunkPath, true
	}
	return d
}

// preferredNonePath / preferredResetPath are sentinel Item.Path values for the
// two synthetic entries in the Workbench-preference picker (ADR-0078). They
// share no prefix with real Workbench items (workbenchItemPathPrefix), so a
// Workbench that happens to be named "<empty>" or "<reset>" is still keyed and
// dispatched correctly.
const (
	preferredNonePath  = "preferred:none"
	preferredResetPath = "preferred:reset"

	preferredNoWorkbenchLabel = "<empty>"
	preferredResetLabel       = "<reset>"
)

// preferredPickerDeps carries the seams for the ctrl+w Workbench-preference
// picker so the write/clear/none branches are unit-testable without a real
// terminal, config, or runtime file.
type preferredPickerDeps struct {
	RunPicker          func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error)
	ResolveWorkbenches func(path string) []config.Workbench
	// CurrentEntry reports the runtime entry for path: present is false when
	// absent (so "<reset>" is offered only when true).
	CurrentEntry   func(path string) (name string, present bool)
	SetPreferred   func(path, name string) error
	ClearPreferred func(path string) error
	// WorkbenchOrder returns the configured [workbench] order tokens that fix the
	// list's display sequence (task 03), or nil for the default order. May be nil
	// (treated as no configured order).
	WorkbenchOrder func() []string
}

// defaultPreferredPickerDeps wires preferredPickerDeps to production
// implementations: the shared Workbench resolution (so bare-repo Workbenches
// still propagate) and the config.runtime.toml [workbench.preferred] store.
func defaultPreferredPickerDeps() *preferredPickerDeps {
	return &preferredPickerDeps{
		RunPicker: ui.Run,
		ResolveWorkbenches: func(path string) []config.Workbench {
			cfgPath := cfgFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				debug.Error("preferred workbench: load config: %v", err)
				return nil
			}
			templates, _ := cfg.ResolveWorkbenchesWith(config.DefaultDeps(), path)
			return templates
		},
		CurrentEntry: func(path string) (string, bool) {
			name, present, err := config.RuntimePreferredWorkbench(path)
			if err != nil {
				debug.Error("preferred workbench: read runtime entry: %v", err)
				return "", false
			}
			return name, present
		},
		SetPreferred:   config.SetRuntimePreferredWorkbench,
		ClearPreferred: config.ClearRuntimePreferredWorkbench,
		WorkbenchOrder: func() []string {
			cfgPath := cfgFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				debug.Error("preferred workbench: load config: %v", err)
				return nil
			}
			return cfg.WorkbenchOrder()
		},
	}
}

// setPreferredWorkbench opens the Workbench-preference picker for checkoutPath
// (ADR-0078): a quick-search list of the Workbenches resolved for that checkout,
// plus "<empty>" (writes explicit none) and, when an entry already
// exists, "<reset>" (deletes the entry). It writes the runtime
// preference for that checkout only — it never touches any running session. Esc
// leaves the preference untouched.
func setPreferredWorkbench(d *preferredPickerDeps, checkoutPath string) error {
	workbenches := d.ResolveWorkbenches(checkoutPath)
	_, hasEntry := d.CurrentEntry(checkoutPath)

	// Build the candidate set in default order — "<empty>", Workbenches in
	// resolution order, then "<reset>" only when a runtime entry exists — and hand
	// it to the shared ordering rule so this picker sequences identically to the
	// create prompt (task 03). "<reset>" is a candidate here only; it is never
	// present in the create prompt regardless of [workbench] order.
	candidates := make([]workbenchOption, 0, len(workbenches)+2)
	candidates = append(candidates, workbenchOption{Label: preferredNoWorkbenchLabel, Item: ui.Item{Name: preferredNoWorkbenchLabel, Path: preferredNonePath}})
	for _, wb := range workbenches {
		candidates = append(candidates, workbenchOption{Label: wb.Name, Item: ui.Item{Name: wb.Name, Path: workbenchItemPathPrefix + wb.Name}})
	}
	if hasEntry {
		candidates = append(candidates, workbenchOption{Label: preferredResetLabel, Item: ui.Item{Name: preferredResetLabel, Path: preferredResetPath}})
	}
	var order []string
	if d.WorkbenchOrder != nil {
		order = d.WorkbenchOrder()
	}
	items := orderWorkbenchOptions(order, candidates)

	result, err := d.RunPicker(items,
		ui.WithHeader("Set preferred workbench (sets the preference only)"),
		ui.WithInitialCursorIndex(0))
	if err != nil {
		return err
	}
	if result.Action != ui.ActionConfirm || result.Selected == nil {
		// Esc/cancel: leave the preference untouched.
		return nil
	}

	switch result.Selected.Path {
	case preferredNonePath:
		return d.SetPreferred(checkoutPath, "")
	case preferredResetPath:
		return d.ClearPreferred(checkoutPath)
	default:
		return d.SetPreferred(checkoutPath, result.Selected.Name)
	}
}

// warnPreferredWorkbenchErr logs a set-preference failure without aborting the
// picker loop — a failed write must not kick the user out of the dashboard.
func warnPreferredWorkbenchErr(surface string, err error) {
	if err != nil {
		debug.Error("%s: set preferred workbench: %v", surface, err)
		fmt.Fprintf(os.Stderr, "Failed to set preferred workbench: %v\n", err)
	}
}
