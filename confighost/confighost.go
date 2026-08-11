// Package confighost opens the Config dashboard inside a program that is not
// `pop config dashboard`: it holds the write side over the real override layer,
// the adapter from resolved override views to the component's rows, and the one
// call a host makes to get a component it can drive (ADR-0202 decisions 10 and
// 11).
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
	"github.com/glebglazov/pop/ui"
)

// Writer is the Config dashboard's write side over the real override layer. It
// is the whole of what the component knows about config: the three actions and
// the re-read that follows each of them.
type Writer struct {
	deps       *config.Deps
	configPath string
}

// NewWriter builds the write side against one hand-authored config path. The
// override layer's own path is derived from deps, not from configPath: the two
// pop-written files sit beside the config dir rather than in it.
func NewWriter(deps *config.Deps, configPath string) Writer {
	return Writer{deps: deps, configPath: configPath}
}

func (w Writer) Store(key, buffer string) (string, error) {
	return config.StoreOverrideBufferWith(w.deps, key, buffer)
}

func (w Writer) CopySource(key string) error {
	return config.CopyOverrideFromSourceWith(w.deps, w.configPath, key)
}

func (w Writer) Remove(key string) error {
	return config.DeleteOverrideValueWith(w.deps, key)
}

func (w Writer) Rows() ([]ui.ConfigDashboardRow, error) {
	views, err := config.OverrideKeyViewsWith(w.deps, w.configPath)
	if err != nil {
		return nil, err
	}
	return Rows(views), nil
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

// Open builds the component a host embeds, over the override-exposed keys as
// they stand now. It never fails: a config that will not resolve becomes the
// component's own error row, because a host may have nowhere safe to print and
// an opened modal saying why is better than a chord that appears to do nothing.
func Open(deps *config.Deps, configPath string) *ui.ConfigDashboard {
	writer := NewWriter(deps, configPath)
	rows, err := writer.Rows()
	m := ui.NewConfigDashboard(rows, ui.ConfigDashboardOpts{Writer: writer})
	if err != nil {
		m.Fail(fmt.Sprintf("Could not read the config keys: %v", err))
	}
	return m
}
