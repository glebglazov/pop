package cmd

import (
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
// is the existing Trunk worktree resolution (non-bare git main worktree, or the
// path a bare repo states via binding.ResolveTrunkPath); a bare repo with no
// trunk anchor yields ("", false) so the inheritance layer is skipped.
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

// preferredPickerDeps carries the seams for the Workbench-preference picker
// `pop workbench prefer` opens, so the write/clear/none branches are
// unit-testable without a real terminal, config, or override file.
type preferredPickerDeps struct {
	RunPicker          func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error)
	ResolveWorkbenches func(path string) []config.Workbench
	// CurrentEntry reports what the repository of path already states: present is
	// false when it states nothing (so "<reset>" is offered only when true).
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
// still propagate) and the override layer's block for the repository, which is
// where a stated preference lives (ADR-0212 decisions 5 and 6).
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
			name, stated, err := config.StatedPreferredWorkbench(path)
			if err != nil {
				debug.Error("preferred workbench: read stated entry: %v", err)
				return "", false
			}
			return name, stated
		},
		SetPreferred:   config.StatePreferredWorkbench,
		ClearPreferred: config.ClearPreferredWorkbench,
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
// plus "<empty>" (states explicit none) and, when the repository already states
// one, "<reset>" (removes what it states). It states the preference for the
// repository of that checkout — it never touches any running session. Esc leaves
// the preference untouched.
//
// `pop workbench prefer` with no name is its only door: the picker hosts opened
// it through a chord of their own until ADR-0212 decision 6 retired that in
// favour of the Config dashboard, which every host already opens.
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
