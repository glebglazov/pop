// Package confighost opens the Config dashboard inside a program that is not
// `pop config dashboard`: it holds the write side over the real override layer
// and over the Convention overlay, the adapter from resolved override views to
// the component's rows, and the one call a host makes to get a component it can
// drive (ADR-0202 decisions 10 and 11; ADR-0212 decision 8).
//
// It exists because the component itself holds no config knowledge and the
// hosts are unrelated programs — the Work dashboard's shell, the project picker
// and the worktree picker. Everything they would each have to re-derive is here
// once.
//
// # The host contract
//
// A host of this component owes it two things, and nothing in the type system
// enforces either (ADR-0202 decision 11):
//
//  1. **Suspend every host key while the component is open.** Not a page toggle,
//     not an action verb, not a modal opener. `pop worktree dashboard` binds
//     ctrl+x to *force delete worktree* and the component binds it to *remove
//     the override*, so a host that keeps its own keys live turns a removed
//     override into a removed worktree. Route key messages to the component
//     alone and hand back what it returns, minus its tea.Quit — that quit closes
//     the modal, not the host.
//  2. **Never let it print.** The component writes nothing to stdout on any
//     path, error included, because in the picker hosts stdout is a data channel
//     (`cd "$(pop worktree dashboard)"`). A host must not print on its behalf
//     either: a failure to build rows or to re-read config belongs in a view, so
//     Open puts it in the component's own error row.
//
// And one thing it owes the human: after the component closes having written,
// re-read config. A host that loaded config once is rendering a value the human
// has just changed (ADR-0202 decision 14). Nothing else is hot-reloaded — the
// supervisor re-reads every pass and each drain it spawns is a fresh process.
package confighost

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/conventions"
	"github.com/glebglazov/pop/ui"
)

// Writer is the Config dashboard's write side over the real override layer. It
// is the whole of what the component knows about config: the three actions and
// the re-read that follows each of them.
//
// It answers for both scopes an override lands at (ADR-0212 decision 3). A key
// of the global surface is read and written whole; a `repo.<key>` row is read
// against the repository the dashboard was opened in and written into the
// layer's block for it — which is how a Preferred workbench, a key with no
// global spelling, is chosen here at all (decision 6). A `conventions.<kind>` row
// is neither: it holds prose, and writing it states the human's Convention
// overlay (decision 8, and conventions.go beside this file).
type Writer struct {
	deps       *config.Deps
	configPath string
	// checkout is the directory the dashboard was opened in, and through it the
	// repository the repo-scope rows and the convention rows are about. Empty
	// means no repository is in scope: those rows are absent and every key is
	// global.
	checkout string
	// conventions is the seam the convention rows resolve and write through.
	conventions *conventions.Deps
}

// NewWriter builds the write side against one hand-authored config path and the
// checkout the dashboard is open in. The override layer's own path is derived
// from deps, not from configPath: the two pop-written files sit beside the config
// dir rather than in it.
func NewWriter(deps *config.Deps, configPath, checkout string) Writer {
	return Writer{
		deps:        deps,
		configPath:  configPath,
		checkout:    checkout,
		conventions: conventionsDeps(deps),
	}
}

// WithConventions points the convention rows at a caller's own seam. A host that
// already resolves conventions for its other verbs passes that one, so the
// dashboard resolves a convention against the same repository the rest of the
// program routes to; a host that has no seam of its own keeps the one derived
// from the config deps.
func (w Writer) WithConventions(cd *conventions.Deps) Writer {
	if cd != nil {
		w.conventions = cd
	}
	return w
}

// repoScope reports the repo-block leaf a row key names, and false for a key of
// the global surface — or for any key when no repository is in scope.
func (w Writer) repoScope(key string) (string, bool) {
	if w.checkout == "" {
		return "", false
	}
	return config.RepoScopeKeyLeaf(key)
}

func (w Writer) Store(key, buffer string) (string, error) {
	if kind, ok := conventions.RowKind(key); ok {
		return w.storeConvention(kind, buffer)
	}
	if _, ok := w.repoScope(key); ok {
		return config.StoreRepoOverrideBufferWith(w.deps, w.checkout, key, buffer)
	}
	return config.StoreOverrideBufferWith(w.deps, key, buffer)
}

func (w Writer) CopySource(key string) error {
	if kind, ok := conventions.RowKind(key); ok {
		return copySourceConvention(kind)
	}
	if _, ok := w.repoScope(key); ok {
		return config.CopyRepoOverrideFromSourceWith(w.deps, w.configPath, w.checkout, key)
	}
	return config.CopyOverrideFromSourceWith(w.deps, w.configPath, key)
}

func (w Writer) Remove(key string) error {
	if kind, ok := conventions.RowKind(key); ok {
		// The dashboard edits one rank and only one — the overlay — so it names
		// that rank to the same writer `pop conventions unset` goes through.
		_, _, err := conventions.Erase(w.conventions, conventions.OriginOverlay, kind, w.checkout)
		return err
	}
	if leaf, ok := w.repoScope(key); ok {
		return config.DeleteRepoOverrideValueWith(w.deps, w.checkout, leaf)
	}
	return config.DeleteOverrideValueWith(w.deps, key)
}

// Rows is the whole list: the keys of the global surface, the repository's own
// keys, then its conventions. Conventions come last because they are the rows a
// reader is least often after; a contested key rises above all of them anyway,
// and the search reaches every row whatever the order.
func (w Writer) Rows() ([]ui.ConfigDashboardRow, error) {
	views, err := config.OverrideKeyViewsWith(w.deps, w.configPath)
	if err != nil {
		return nil, err
	}
	repoViews, err := config.RepoOverrideKeyViews(w.deps, w.configPath, w.checkout)
	if err != nil {
		return nil, err
	}
	return append(Rows(append(views, repoViews...)), w.conventionRows()...), nil
}

// Rows adapts the resolved override views to the component's rows. The
// component holds no config knowledge — provenance and the words that tell two
// empty-looking states apart are decided in config, so `pop config dashboard`
// and every host that embeds the component say the same thing.
func Rows(views []config.OverrideKeyView) []ui.ConfigDashboardRow {
	rows := make([]ui.ConfigDashboardRow, 0, len(views))
	for _, view := range views {
		reach := make([]ui.ConfigDashboardReachLine, 0, len(view.Reach))
		for _, line := range view.Reach {
			reach = append(reach, ui.ConfigDashboardReachLine{Actor: line.Actor, Detail: line.Detail})
		}
		if len(reach) == 0 {
			reach = nil
		}
		rows = append(rows, ui.ConfigDashboardRow{
			Key:        view.Key,
			Desc:       view.Desc,
			Overridden: view.Overridden,
			Contested:  view.Contested,
			Preview: ui.ConfigDashboardPreview{
				ValueTOML:        view.EffectiveTOML,
				Provenance:       view.Provenance(),
				Note:             view.Note,
				SourceTOML:       view.SourceTOML,
				SourceProvenance: view.SourceProvenance(),
				Reach:            reach,
			},
		})
	}
	return rows
}

// WorkingCheckout is the checkout a host with no directory of its own opens the
// dashboard in — where pop is running. A host that knows better (a command with
// its own --dir) passes that instead.
func WorkingCheckout(deps *config.Deps) string {
	if deps == nil || deps.FS == nil {
		return ""
	}
	cwd, err := deps.FS.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// Open builds the component a host embeds, over the overridable keys as
// they stand now. It never fails: a config that will not resolve becomes the
// component's own error row, because a host may have nowhere safe to print and
// an opened modal saying why is better than a chord that appears to do nothing.
func Open(deps *config.Deps, configPath, checkout string) *ui.ConfigDashboard {
	writer := NewWriter(deps, configPath, checkout)
	rows, err := writer.Rows()
	m := ui.NewConfigDashboard(rows, ui.ConfigDashboardOpts{Writer: writer})
	if err != nil {
		m.Fail(fmt.Sprintf("Could not read the config keys: %v", err))
	}
	return m
}
