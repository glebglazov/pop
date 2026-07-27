package queue

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
)

// WayfinderSpawnResult is the outcome of spawning an attended wayfinder session.
type WayfinderSpawnResult struct {
	PaneID   string
	MapID    string
	TicketID string
}

// LaunchWayfinderSession spawns an attended wayfinder session for map row in a
// new tmux window named after the map inside the repo's session (ADR-0130). An
// empty ticketID targets the next frontier ticket; a non-empty ticketID must name
// a frontier ticket. Pop does not write ticket files — the session self-claims.
func LaunchWayfinderSession(d *Deps, cfg *config.Config, row DashboardRow, ticketID string) (WayfinderSpawnResult, error) {
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
		return WayfinderSpawnResult{}, fmt.Errorf("not a wayfinder map row")
	}
	storageDir := dashboardRowStorageDir(row)
	if storageDir == "" {
		return WayfinderSpawnResult{}, fmt.Errorf("no storage dir for map %q", row.SetID)
	}
	wd := &wayfinder.Deps{FS: d.Tasks.FS, Tasks: d.Tasks}
	maps, err := wayfinder.ScanMapsInStorage(wd, storageDir)
	if err != nil {
		return WayfinderSpawnResult{}, err
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
		return WayfinderSpawnResult{}, fmt.Errorf("map %q not found", row.SetID)
	}
	ticket, err := wayfinder.TargetTicket(*wfMap, ticketID)
	if err != nil {
		return WayfinderSpawnResult{}, err
	}

	base := strings.TrimSpace(row.ProjectPath)
	if base == "" {
		return WayfinderSpawnResult{}, fmt.Errorf("no project path for map %q", row.SetID)
	}
	preset := tasks.ResolveDefaultInteractiveAgentPreset(cfg)
	skillsPrefix := config.DefaultSkillsPrefix
	if cfg != nil {
		skillsPrefix = cfg.ResolveSkillsPrefix()
	}
	prompt := wayfinder.WorkModeInvocation(skillsPrefix, wfMap.ID, ticket.ID)
	invocation, err := tasks.ResolveAgentAssistanceInvocation(preset, "", prompt, base)
	if err != nil {
		return WayfinderSpawnResult{}, fmt.Errorf("resolve interactive agent: %w", err)
	}
	command := attendedShellCommand(invocation)
	session := project.SessionNameWith(d.Project, base)
	paneID, err := spawnWayfinderWindow(d.Tmux, session, base, wfMap.ID, command)
	if err != nil {
		return WayfinderSpawnResult{}, err
	}
	return WayfinderSpawnResult{
		PaneID:   paneID,
		MapID:    wfMap.ID,
		TicketID: ticket.ID,
	}, nil
}

// spawnWayfinderWindow creates the repo session when absent and lands command in
// a window named after the map. It never uses the pop-queue drain window: an
// existing map window has its single pane reused (the command is re-sent), a
// missing one is created.
func spawnWayfinderWindow(tmux tmuxmod.Tmux, session, dir, windowName, command string) (string, error) {
	paneID, _, err := tmuxmod.EnsureWindow(tmux, session, windowName, dir)
	if err != nil {
		return "", err
	}
	if err := tmux.SendKeys(paneID, command, "Enter"); err != nil {
		return "", fmt.Errorf("send wayfinder command: %w", err)
	}
	return paneID, nil
}

func attendedShellCommand(inv *tasks.AgentAssistanceInvocation) string {
	if inv == nil {
		return ""
	}
	parts := []string{shellQuote(inv.Command.Name)}
	for _, arg := range inv.Command.Args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}
