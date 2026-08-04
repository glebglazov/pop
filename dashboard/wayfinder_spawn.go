package dashboard

import (
	"fmt"
	"github.com/glebglazov/pop/tasks/drain"
	"strings"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/wayfinder"
)

// LaunchWayfinderSession spawns an attended Grilling pane for map row inside the
// Map's own tmux session, through the same composite `pop map next` uses — the
// dashboard and the verb are two doors into one session, not two spawners. An
// empty ticketID targets the next frontier ticket; a non-empty ticketID must name
// a frontier ticket. The claim is taken for the spawned pane, so the agent's own
// `pop map claim` renews it rather than being refused (ADR-0182). A running
// grilling pane is a jump target: focus it rather than re-sending work
// (ADR-0158); an idle one (bare shell) respawns the command.
func LaunchWayfinderSession(d *drain.Deps, cfg *config.Config, row DashboardRow, ticketID string) (drain.DashboardDrainResult, error) {
	wd, wfMap, _, err := wayfinderSpawnTarget(d, cfg, row)
	if err != nil {
		return drain.DashboardDrainResult{}, err
	}
	ticket, err := wayfinder.TargetTicket(*wfMap, ticketID)
	if err != nil {
		return drain.DashboardDrainResult{}, err
	}
	spawned, err := wayfinder.SpawnTicket(wd, cfg, *wfMap, ticket)
	if err != nil {
		return drain.DashboardDrainResult{}, err
	}
	return drain.DashboardDrainResult{PaneID: spawned.Pane.PaneID, Session: spawned.Pane.Session.Name}, nil
}

// LaunchWayfinderFanOut spawns one Grilling pane per frontier ticket — the same
// per-ticket spawn, looped. It reports the first pane so the focusing key has
// somewhere to land, plus how many tickets went out. An empty frontier reads as
// ErrEmptyFrontier, which the dashboard already surfaces as a status line.
func LaunchWayfinderFanOut(d *drain.Deps, cfg *config.Config, row DashboardRow) (drain.DashboardDrainResult, int, error) {
	wd, wfMap, _, err := wayfinderSpawnTarget(d, cfg, row)
	if err != nil {
		return drain.DashboardDrainResult{}, 0, err
	}
	out, err := wayfinder.SpawnFrontier(wd, cfg, *wfMap, 0)
	if err != nil {
		return drain.DashboardDrainResult{}, 0, err
	}
	if len(out.Spawned) == 0 {
		return drain.DashboardDrainResult{}, 0, wayfinder.ErrEmptyFrontier
	}
	first := out.Spawned[0].Pane
	return drain.DashboardDrainResult{PaneID: first.PaneID, Session: first.Session.Name}, len(out.Spawned), nil
}

// wayfinderSpawnTarget resolves what both spawn verbs need off a map row: the
// wayfinder deps its session is rooted through, the Map itself, and the checkout
// the row names.
func wayfinderSpawnTarget(d *drain.Deps, cfg *config.Config, row DashboardRow) (*wayfinder.Deps, *wayfinder.Map, string, error) {
	if d == nil {
		d = drain.DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = tmuxmod.New()
	}
	if !mapRow(row) {
		return nil, nil, "", fmt.Errorf("not a wayfinder map row")
	}
	storageDir := dashboardRowStorageDir(row)
	if storageDir == "" {
		return nil, nil, "", fmt.Errorf("no storage dir for map %q", row.ID)
	}
	base := strings.TrimSpace(row.ProjectPath)
	if base == "" {
		return nil, nil, "", fmt.Errorf("no project path for map %q", row.ID)
	}
	wd := &wayfinder.Deps{
		FS:    d.Tasks.FS,
		Tasks: d.Tasks,
		Tmux:  d.Tmux,
		Trunk: func() (string, error) { return mapSessionTrunk(d, cfg, base) },
	}
	maps, err := wayfinder.ScanMapsInStorage(wd, storageDir)
	if err != nil {
		return nil, nil, "", err
	}
	for i := range maps {
		if maps[i].ID == row.ID {
			cp := maps[i]
			return wd, &cp, base, nil
		}
	}
	return nil, nil, "", fmt.Errorf("map %q not found", row.ID)
}

// mapSessionTrunk resolves the Trunk worktree a Map's session is rooted at from
// the dashboard row's checkout, the same resolution managed Task-set
// registration performs. The dashboard has no --trunk to offer, so an
// unresolvable Trunk falls back to the checkout the row already names rather
// than refusing the launch outright.
func mapSessionTrunk(d *drain.Deps, cfg *config.Config, checkout string) (string, error) {
	path, bare, err := binding.ResolveTrunkPathWith(nil, d.Tasks, cfg, checkout)
	if err != nil || bare || strings.TrimSpace(path) == "" {
		return checkout, nil
	}
	return path, nil
}
