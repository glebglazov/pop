package wayfinder

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Map Work kind: the adapter that makes a Map comply with `work.Kind`. It
// lives here, beside the Map's own verbs, because the seam's import direction is
// one-way — kinds comply and `work` imports no kind — and because every verb it
// offers is a call the Map lifecycle already owns.

// The Map's four frontier verbs are two acts — one ticket or the whole frontier —
// each offered twice, going and staying. Spawning a Grilling pane never moves the
// operator on its own (ADR-0182), so the move is what the key says: uppercase
// hands off, lowercase acts in place (ADR-0158's case rule, unchanged).
const (
	// VerbWork claims a Map's frontier ticket (or the named one), opens its
	// Grilling pane, and takes the operator there.
	VerbWork work.Verb = "work-ticket"
	// VerbWorkHere is VerbWork without the move.
	VerbWorkHere work.Verb = "work-ticket-here"
	// VerbFanOut spawns a Grilling pane for every frontier ticket and takes the
	// operator to the Map's window.
	VerbFanOut work.Verb = "fan-out-frontier"
	// VerbFanOutHere is VerbFanOut without the move.
	VerbFanOutHere work.Verb = "fan-out-frontier-here"
	// VerbAssist opens the Map's own attended session — no ticket, no claim — and
	// takes the operator there. It has no staying twin: fanning out N tickets is
	// followed by more triage, whereas an assist pane is one session you asked for
	// in order to go talk to it (ADR-0184).
	VerbAssist work.Verb = "assist-map"
)

// MapKindDeps holds what the Map kind reads. The wayfinder deps carry the Map
// side (filesystem, store, tmux, Trunk); Project and Config are what repository
// group resolution needs.
type MapKindDeps struct {
	Wayfinder *Deps
	Project   *project.Deps
	Config    *config.Config
	// Groups resolves the repository groups to scan for Maps. Defaults to
	// repogroup.Resolve over Wayfinder.Tasks and Project — injectable because a
	// test wants to name the groups rather than lay out a machine.
	Groups func() ([]repogroup.Group, error)
}

// MapKind is the Map Work kind.
type MapKind struct {
	d *MapKindDeps
}

// NewMapKind returns the Map kind over d. A nil d resolves to real dependencies.
func NewMapKind(d *MapKindDeps) *MapKind {
	if d == nil {
		d = &MapKindDeps{}
	}
	if d.Wayfinder == nil {
		d.Wayfinder = DefaultDeps()
	}
	if d.Wayfinder.Tasks == nil {
		d.Wayfinder.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	return &MapKind{d: d}
}

// ID is the closed enum's map member.
func (k *MapKind) ID() work.KindID { return ref.KindMap }

// Load reads every Map still worth looking at, per repository group: active and
// arrived. An arrived Map stays — it is the lineage view for the sets it spawned,
// and Archive is the hide mechanism (ADR-0172). Abandoned, archived and BROKEN
// Maps are hidden.
func (k *MapKind) Load() ([]work.Container, error) {
	groups, err := k.groups()
	if err != nil {
		return nil, err
	}
	var containers []work.Container
	for _, g := range groups {
		storage := g.Storage()
		if storage == "" {
			continue
		}
		maps, err := ScanMapsInStorage(k.d.Wayfinder, storage)
		if err != nil {
			return nil, err
		}
		// One table per group: every Map of a repository spawns into that
		// repository's Task storage, so the sets are read once for all of them.
		sets := newSetStatusTable(k.d.Wayfinder, g.DefPath)
		for _, m := range maps {
			if !visible(m) {
				continue
			}
			containers = append(containers, containerFor(g, m, sets.resolve(m.SpawnedSets)))
		}
	}
	return containers, nil
}

// Less orders two Maps: by project label, then by id descending — the newest Map
// of a project first. It is the Map's own comparator and nothing else's; the seam
// never asks it to rank a Map against a task set.
func (k *MapKind) Less(a, b work.Container) bool {
	if a.Project != b.Project {
		return a.Project < b.Project
	}
	return a.ID > b.ID
}

// Summary returns the Map kind's header phrase, or nothing when a page has no
// Maps: a machine with no wayfinding in flight should not be told so.
func (k *MapKind) Summary(containers []work.Container) []string {
	if len(containers) == 0 {
		return nil
	}
	return []string{work.CountPhrase(len(containers), "map", "maps")}
}

// Columns are the headers a page of Maps reads under. They are the Task-set
// column headers verbatim, because a Map row fills the Task-set cells rather than
// a set of its own — repeated here rather than imported from the Task-set kind,
// since one kind importing another to borrow five strings is a worse coupling
// than the conformance test that pins the two lists equal.
func (k *MapKind) Columns() []string {
	return []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}
}

// Actions returns the container-level verbs for a Map: work one frontier ticket
// or fan out the whole frontier when it has one, assist the Map itself, a shell in
// the repository, and copy-name. Spawning verbs come before in-place ones, and all
// four frontier keys are gated on a frontier — a Map with none offers no dead key.
// Assist is **not** gated: a Map whose frontier is empty or fully claimed is when a
// session scoped to the Map itself is most needed (ADR-0184). The Task-set verbs
// (drain/bind/…) have never applied to a Map and still do not — the shared two are
// the only verbs a Map has in common with a task set.
func (k *MapKind) Actions(c work.Container) []work.Action {
	var actions []work.Action
	if c.MapFrontier > 0 {
		actions = append(actions,
			work.Action{Verb: VerbWork, Key: "I", Label: "work frontier ticket and go"},
			work.Action{Verb: VerbFanOut, Key: "A", Label: "fan out frontier and go"},
		)
	}
	actions = append(actions,
		work.Action{Verb: VerbAssist, Key: "S", Label: "assist the map and go"},
		work.Action{Verb: work.VerbShell, Key: "O", Label: "shell"},
	)
	if c.MapFrontier > 0 {
		actions = append(actions,
			work.Action{Verb: VerbWorkHere, Key: "i", Label: "work frontier ticket"},
			work.Action{Verb: VerbFanOutHere, Key: "a", Label: "fan out frontier"},
		)
	}
	return append(actions, work.Action{Verb: work.VerbCopyName, Key: "y", Label: "copy name"})
}

// ItemActions returns the verbs for one Decision ticket: work it — going or
// staying — when it is on the frontier, since a resolved or blocked ticket cannot
// be worked, and copy-name. Fanning out is a container act and is offered nowhere
// else.
func (k *MapKind) ItemActions(c work.Container, item work.Item) []work.Action {
	var actions []work.Action
	if item.Status == string(TicketOpen) && !item.Blocked {
		actions = append(actions,
			work.Action{Verb: VerbWork, Key: "I", Label: "work ticket and go"},
			work.Action{Verb: VerbWorkHere, Key: "i", Label: "work ticket"},
		)
	}
	return append(actions, work.Action{Verb: work.VerbCopyName, Key: "y", Label: "copy name"})
}

// Perform runs one Map verb. Working a ticket goes through the same composite
// `pop map next` uses — the dashboard and the verb are two doors into one
// session, not two spawners — and returns the handoff the caller switches to.
func (k *MapKind) Perform(c work.Container, item *work.Item, verb work.Verb) (work.Outcome, error) {
	switch verb {
	case work.VerbCopyName:
		payload := c.ID
		if item != nil {
			payload = item.ID
		}
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: payload, Message: "copied " + payload}, nil
	case work.VerbShell:
		dir := strings.TrimSpace(c.Checkout)
		if dir == "" {
			return work.Outcome{}, fmt.Errorf("wayfinder: map %q has no checkout to open a shell in", c.ID)
		}
		return work.Outcome{
			Kind:    work.OutcomeHandoff,
			Handoff: work.Handoff{Kind: work.HandoffTmux, Dir: dir},
			Message: "shell in " + dir,
		}, nil
	case VerbWork, VerbWorkHere:
		ticketID := ""
		if item != nil {
			ticketID = item.ID
		}
		return k.workTicket(c, ticketID, verb == VerbWork)
	case VerbFanOut, VerbFanOutHere:
		return k.fanOutFrontier(c, verb == VerbFanOut)
	case VerbAssist:
		return k.assistMap(c)
	default:
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
}

// workTicket opens the Grilling pane for a Map's ticket inside the Map's own tmux
// session. An empty ticketID targets the next frontier ticket; a named one must be
// on the frontier. A running pane is a jump target, reported rather than re-sent
// (ADR-0158). focus decides whether the operator is taken there at all.
func (k *MapKind) workTicket(c work.Container, ticketID string, focus bool) (work.Outcome, error) {
	target, wd, checkout, err := k.resolveMapForSpawn(c)
	if err != nil {
		return work.Outcome{}, err
	}
	ticket, err := TargetTicket(*target, ticketID)
	if err != nil {
		return work.Outcome{}, err
	}
	spawned, err := SpawnTicket(wd, k.d.Config, *target, ticket)
	if err != nil {
		return work.Outcome{}, err
	}
	message := fmt.Sprintf("grilling %s/%s in %s", target.ID, ticket.ID, spawned.Pane.Session.Name)
	return spawnOutcome(spawned.Pane.Session.Name, checkout, message, focus), nil
}

// fanOutFrontier spawns one Grilling pane per frontier ticket. An empty frontier
// reads as ErrEmptyFrontier, the same answer the single-ticket verb gives, so the
// dashboard reports it as a status line rather than as a failure.
func (k *MapKind) fanOutFrontier(c work.Container, focus bool) (work.Outcome, error) {
	target, wd, checkout, err := k.resolveMapForSpawn(c)
	if err != nil {
		return work.Outcome{}, err
	}
	out, err := SpawnFrontier(wd, k.d.Config, *target, 0)
	if err != nil {
		return work.Outcome{}, err
	}
	if len(out.Spawned) == 0 {
		return work.Outcome{}, ErrEmptyFrontier
	}
	message := fmt.Sprintf("fanned out %d of %s into %s", len(out.Spawned), target.ID, out.Session.Name)
	return spawnOutcome(out.Session.Name, checkout, message, focus), nil
}

// assistMap opens the Map's own attended session and hands it off. It reads no
// frontier and takes no claim, so it is the one Map verb that works on a Map in
// any state (ADR-0184). A live assist pane is a jump target rather than a second
// session, which is what keeps two conversations off one Map's prose.
func (k *MapKind) assistMap(c work.Container) (work.Outcome, error) {
	target, wd, checkout, err := k.resolveMapForSpawn(c)
	if err != nil {
		return work.Outcome{}, err
	}
	pane, err := SpawnAssist(wd, k.d.Config, *target)
	if err != nil {
		return work.Outcome{}, err
	}
	verb := "assisting"
	if pane.Reused {
		verb = "returning to the assist session for"
	}
	message := fmt.Sprintf("%s %s in %s", verb, target.ID, pane.Session.Name)
	return spawnOutcome(pane.Session.Name, checkout, message, true), nil
}

// spawnOutcome is the fork the case rule turns on: a focusing verb hands the
// session off, a staying one reports the same sentence and leaves the operator
// where they were.
func spawnOutcome(session, checkout, message string, focus bool) work.Outcome {
	if !focus {
		return work.Outcome{Kind: work.OutcomeMessage, Message: message}
	}
	return work.Outcome{
		Kind: work.OutcomeHandoff,
		Handoff: work.Handoff{
			Kind:   work.HandoffTmux,
			Target: session,
			Dir:    checkout,
		},
		Message: message,
	}
}

// resolveMapForSpawn reads the Map a spawn verb acts on off the container row,
// along with the deps its session is rooted through and the checkout the row
// names.
func (k *MapKind) resolveMapForSpawn(c work.Container) (*Map, *Deps, string, error) {
	storage := storageDirFor(c)
	if storage == "" {
		return nil, nil, "", fmt.Errorf("wayfinder: no storage dir for map %q", c.ID)
	}
	checkout := strings.TrimSpace(c.Checkout)
	if checkout == "" {
		return nil, nil, "", fmt.Errorf("wayfinder: no checkout for map %q", c.ID)
	}
	wd := k.sessionDeps(checkout)
	maps, err := ScanMapsInStorage(wd, storage)
	if err != nil {
		return nil, nil, "", err
	}
	for i := range maps {
		if maps[i].ID == c.ID {
			return &maps[i], wd, checkout, nil
		}
	}
	return nil, nil, "", fmt.Errorf("wayfinder: map %q not found", c.ID)
}

// sessionDeps returns the Map deps a session verb runs with, resolving the Trunk
// worktree the session is rooted at from the container's checkout when the caller
// wired no Trunk of its own. A dashboard has no `--trunk` to offer, so an
// unresolvable Trunk falls back to the checkout the container names rather than
// refusing the launch outright.
func (k *MapKind) sessionDeps(checkout string) *Deps {
	wd := k.d.Wayfinder
	if wd.Trunk != nil {
		return wd
	}
	clone := *wd
	clone.Trunk = func() (string, error) {
		path, bare, err := binding.ResolveTrunkPathWith(nil, wd.taskDeps(), k.d.Config, checkout)
		if err != nil || bare || strings.TrimSpace(path) == "" {
			return checkout, nil
		}
		return path, nil
	}
	return &clone
}

// groups resolves the repository groups to scan, through the injected seam when
// one was supplied.
func (k *MapKind) groups() ([]repogroup.Group, error) {
	if k.d.Groups != nil {
		return k.d.Groups()
	}
	return repogroup.Resolve(&repogroup.Deps{Tasks: k.d.Wayfinder.Tasks, Project: k.d.Project}, k.d.Config)
}

// containerFor projects one Map onto a Work container. The status cell is the
// Map's ticket tallies, which is the only status a Map has ever shown on a read
// surface: `WAYFINDING · N open / M frontier` (ADR-0130).
func containerFor(g repogroup.Group, m Map, spawned []SpawnedSet) work.Container {
	counts := CountTickets(m.Tickets)
	frontier := len(Frontier(m.Tickets))
	return work.Container{
		Kind:           ref.KindMap,
		ID:             m.ID,
		Project:        g.ProjectName,
		Status:         mapStatusLabel,
		Checkout:       g.CheckoutPath(),
		CursorKey:      g.ProjectName + "\x00map\x00" + m.ID,
		Broken:         m.Broken,
		BrokenReason:   m.BrokenReason,
		Items:          itemsFor(m),
		DetailSections: sectionsFor(m, spawned),
		DefPath:        g.DefPath,
		StatePath:      g.StatePath,
		RepoKey:        g.RepoKey,
		RepoCommonDir:  g.RepoCommonDir,
		ProjectPath:    g.CheckoutPath(),
		MapOpen:        counts.Open,
		MapFrontier:    frontier,
	}
}

// StatusCell composes a Map's STATUS cell: the one label a Map shows, then its
// ticket tallies (ADR-0130). The tallies are plain — how much thinking is left is
// a quantity, not an alarm — so only the label carries a tone.
func (k *MapKind) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{
		{Text: mapStatusLabel, Tone: work.ToneLabel},
		{Text: fmt.Sprintf("%d open / %d frontier", c.MapOpen, c.MapFrontier), Tone: work.TonePlain},
	}
}

// mapStatusLabel is the one status label a Map shows on a read surface. A Map's
// lifecycle status (active/arrived) is not it: what a reader needs from a row is
// how much thinking is left, which is what the ticket tallies beside this label
// say.
const mapStatusLabel = "WAYFINDING"

// itemsFor projects a Map's Decision tickets onto Work items. A ticket is blocked
// when it is still open behind an unresolved blocker — the same rule the frontier
// applies, expressed per item.
func itemsFor(m Map) []work.Item {
	byID := make(map[string]TicketStatus, len(m.Tickets))
	for _, t := range m.Tickets {
		byID[t.ID] = t.Status
	}
	items := make([]work.Item, 0, len(m.Tickets))
	for _, t := range m.Tickets {
		blocked := false
		if t.Status == TicketOpen {
			for _, b := range t.BlockedBy {
				if byID[b] != TicketResolved {
					blocked = true
					break
				}
			}
		}
		items = append(items, work.Item{
			ID:        t.ID,
			Title:     ticketItemTitle(t),
			Type:      string(t.Type),
			Status:    string(t.Status),
			Blocked:   blocked,
			BlockedBy: t.BlockedBy,
			File:      filepath.Join(m.Dir, "issues", t.File),
		})
	}
	return items
}

// sectionsFor is the Map's prose for a detail view: where it is going, what it
// has settled so far, and what the effort spawned. The first two come off map.md;
// an empty one is omitted rather than rendered as an empty heading. The spawned
// sets are the resolved lineage lines — the payoff of recording the ids, which is
// seeing from the Map whether the work it handed off has landed.
func sectionsFor(m Map, spawned []SpawnedSet) []work.Section {
	var sections []work.Section
	if strings.TrimSpace(m.Destination) != "" {
		sections = append(sections, work.Section{Title: "Destination", Body: m.Destination})
	}
	if strings.TrimSpace(m.DecisionsSoFar) != "" {
		sections = append(sections, work.Section{Title: "Decisions so far", Body: m.DecisionsSoFar})
	}
	if len(spawned) > 0 {
		lines := make([]string, 0, len(spawned))
		for _, s := range spawned {
			lines = append(lines, s.Line())
		}
		sections = append(sections, work.Section{Title: "Spawned sets", Body: strings.Join(lines, "\n")})
	}
	return sections
}

// visible reports whether a Map should appear as a Work container (ADR-0130,
// ADR-0172).
func visible(m Map) bool {
	if m.Archived || m.Broken {
		return false
	}
	return m.Status == MapActive || m.Status == MapArrived
}

// storageDirFor derives a Map container's Task-storage directory from the
// definition path its repository group carried.
func storageDirFor(c work.Container) string {
	if c.DefPath == "" {
		return ""
	}
	return filepath.Dir(c.DefPath)
}
