package tasks

import "fmt"

// PreparedRefresh is one definition path with the halves of a refresh that must
// not run beside another group's already taken: the storage-layout migration,
// which writes, and the registration read, which goes through pop's single store
// connection (ADR-0140). What is left is filesystem-bound and pure — discovery,
// manifest loading, row building — which is what a Work read surface fans out
// per repository group (ADR-0189).
//
// One of these is per definition path, but the registration view inside is not:
// registration is machine-global, so every PreparedRefresh from one
// PrepareRefreshes call shares a single read of it. That view is never mutated
// after preparation, which is what makes concurrent readers safe.
type PreparedRefresh struct {
	defPath string
	state   *GlobalState
}

// PrepareRefreshes takes the store-side and write-side half of a refresh for
// every definition path a load is about to read, serially and to completion, and
// returns one prepared value per path in the order given. Nothing here may run
// concurrently: the migration writes, the legacy state fold writes, and both
// speak to the one store connection.
//
// The registration read is machine-global and so is taken once for the whole
// batch rather than once per path.
func PrepareRefreshes(d *Deps, defPaths []string) ([]*PreparedRefresh, error) {
	prepared := make([]*PreparedRefresh, 0, len(defPaths))
	for _, defPath := range defPaths {
		canon, err := migrateForRefresh(d, defPath)
		if err != nil {
			return nil, err
		}
		// The fold retires the per-repository state.json this path still carries, so
		// unlike the read below it is per path.
		if err := foldLegacyStateFile(d, StatePathFor(canon)); err != nil {
			return nil, err
		}
		prepared = append(prepared, &PreparedRefresh{defPath: canon})
	}
	if len(prepared) == 0 {
		return nil, nil
	}
	state, err := readGlobalState(d, StatePathFor(prepared[0].defPath))
	if err != nil {
		return nil, err
	}
	for _, p := range prepared {
		p.state = state
	}
	return prepared, nil
}

// DefinitionPath is the canonical definition path this value was prepared for —
// canonical because migration may have created the directory the caller named,
// and every key derived from it must match what migration wrote.
func (p *PreparedRefresh) DefinitionPath() string {
	if p == nil {
		return ""
	}
	return p.defPath
}

// Refresh reads this definition path's rows from the prepared half: discovery,
// manifests and row building, and nothing else. It opens no store and writes
// nothing, so several prepared paths may be refreshed at the same time.
func (p *PreparedRefresh) Refresh(d *Deps) (*RefreshResult, error) {
	return p.refresh(d, archivedHidden)
}

// RefreshIncludingArchived is Refresh over archived and active sets together —
// the Work dashboard's show-archived view (ADR-0186).
func (p *PreparedRefresh) RefreshIncludingArchived(d *Deps) (*RefreshResult, error) {
	return p.refresh(d, archivedIncluded)
}

func (p *PreparedRefresh) refresh(d *Deps, archived archivedRows) (*RefreshResult, error) {
	if p == nil || p.state == nil {
		return nil, fmt.Errorf("tasks: refresh of an unprepared definition path")
	}
	disc, err := discoverForRefresh(d, p.defPath)
	if err != nil {
		return nil, err
	}
	return buildRefreshResult(d, p.defPath, disc, p.state, archived), nil
}

// WorktreeIntents returns the seeded worktree directives registered under this
// definition path, keyed by set id, from the registration this value was
// prepared with. It is the same answer RegisteredWorktreeIntent gives one set at
// a time, served without the store read that would cost — a read a page makes
// once per row otherwise (ADR-0189).
func (p *PreparedRefresh) WorktreeIntents() map[string]*WorktreeDirective {
	intents := map[string]*WorktreeDirective{}
	if p == nil || p.state == nil {
		return intents
	}
	entry := p.state.Tasks[p.defPath]
	if entry == nil {
		return intents
	}
	for _, set := range entry.TaskSets {
		if set.WorktreeIntent != nil {
			intents[set.ID] = set.WorktreeIntent
		}
	}
	return intents
}
