package queue

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/wayfinder"
)

// LaunchWayfinderSession spawns an attended grilling window for map row inside
// the Map's own tmux session, through the same composite `pop map next` uses —
// the dashboard and the verb are two doors into one session, not two spawners.
// An empty ticketID targets the next frontier ticket; a non-empty ticketID must
// name a frontier ticket. Pop does not write ticket files — the session
// self-claims. A running grilling window is a jump target: focus it rather than
// re-sending work (ADR-0158). An idle window (bare shell) respawns the command.
func LaunchWayfinderSession(d *Deps, cfg *config.Config, row DashboardRow, ticketID string) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
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
	if !row.IsMap {
		return DashboardDrainResult{}, fmt.Errorf("not a wayfinder map row")
	}
	storageDir := dashboardRowStorageDir(row)
	if storageDir == "" {
		return DashboardDrainResult{}, fmt.Errorf("no storage dir for map %q", row.SetID)
	}
	base := strings.TrimSpace(row.ProjectPath)
	if base == "" {
		return DashboardDrainResult{}, fmt.Errorf("no project path for map %q", row.SetID)
	}
	wd := &wayfinder.Deps{
		FS:    d.Tasks.FS,
		Tasks: d.Tasks,
		Tmux:  d.Tmux,
		Trunk: func() (string, error) { return mapSessionTrunk(d, cfg, base) },
	}
	maps, err := wayfinder.ScanMapsInStorage(wd, storageDir)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	var wfMap *wayfinder.Map
	for i := range maps {
		if maps[i].ID == row.SetID {
			cp := maps[i]
			wfMap = &cp
			break
		}
	}
	if wfMap == nil {
		return DashboardDrainResult{}, fmt.Errorf("map %q not found", row.SetID)
	}
	ticket, err := wayfinder.TargetTicket(*wfMap, ticketID)
	if err != nil {
		return DashboardDrainResult{}, err
	}

	command, err := wayfinder.GrillingInvocation(cfg, wfMap.ID, ticket.ID, base)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	win, err := wayfinder.OpenGrillingWindow(wd, wfMap.ID, ticket, command)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: win.PaneID, Session: win.Session.Name}, nil
}

// mapSessionTrunk resolves the Trunk worktree a Map's session is rooted at from
// the dashboard row's checkout, the same resolution managed Task-set
// registration performs. The dashboard has no --trunk to offer, so an
// unresolvable Trunk falls back to the checkout the row already names rather
// than refusing the launch outright.
func mapSessionTrunk(d *Deps, cfg *config.Config, checkout string) (string, error) {
	path, bare, err := binding.ResolveTrunkPathWith(nil, d.Tasks, cfg, checkout)
	if err != nil || bare || strings.TrimSpace(path) == "" {
		return checkout, nil
	}
	return path, nil
}
