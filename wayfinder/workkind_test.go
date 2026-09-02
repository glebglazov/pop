package wayfinder

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// mapKindFixture lays out one repository group holding five Maps — active,
// arrived, abandoned, archived (through the retired archive side-file) and one
// BROKEN by a retired Status: word — and returns the kind over it.
func mapKindFixture(t *testing.T) (*MapKind, repogroup.Group) {
	t.Helper()
	storageDir := "/data/repos/repo-aaaa"
	tasksDir := filepath.Join(storageDir, "tasks")
	activeMap := filepath.Join(storageDir, "maps", "2026-07-01-active")
	arrivedMap := filepath.Join(storageDir, "maps", "2026-07-02-arrived")
	abandonedMap := filepath.Join(storageDir, "maps", "2026-07-03-abandoned")
	archivedMap := filepath.Join(storageDir, "maps", "2026-07-04-archived")
	brokenMap := filepath.Join(storageDir, "maps", "2026-07-05-broken")
	files := map[string]string{
		filepath.Join(activeMap, "map.md"): "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues", "01-research.md"): "" +
			"Type: research\nStatus: open\n\n# Q\n",
		filepath.Join(activeMap, "issues", "02-blocked.md"): "" +
			"Type: research\nStatus: open\nBlocked by: 01\n\n# Q\n",
		filepath.Join(arrivedMap, "map.md"):                 "Status: arrived\n\n## Destination\nArrived\n",
		filepath.Join(abandonedMap, "map.md"):               "Status: abandoned\n\n## Destination\nNope\n",
		filepath.Join(archivedMap, "map.md"):                "Status: active\n\n## Destination\nHidden\n",
		filepath.Join(brokenMap, "map.md"):                  "Status: done\n\n## Destination\nRetired word\n",
		filepath.Join(storageDir, "wayfinder-archive.json"): `{"archived":["2026-07-04-archived"]}`,
	}
	wd := wayfinderTestDeps(t, t.TempDir(), "/repo/.git", files)
	group := repogroup.Group{
		DefPath:       tasksDir,
		StatePath:     tasks.StatePathFor(tasksDir),
		StorageDir:    storageDir,
		RepoKey:       "repo-key",
		RepoCommonDir: "/repo/.git",
		ProjectName:   "pop",
		Rep:           &repogroup.Checkout{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
	}
	k := NewMapKind(&MapKindDeps{
		Wayfinder: wd,
		Config:    &config.Config{},
		Groups:    func() ([]repogroup.Group, error) { return []repogroup.Group{group}, nil },
	})
	return k, group
}

// TestMapKindLoadsVisibleMapsOnly pins what a Map contributes as a Work
// container: an arrived Map stays beside the active one — it is the lineage view
// for the sets it spawned, and Archive is what hides a Map (ADR-0172) — while
// abandoned, archived and BROKEN Maps are hidden, and the container's status cell
// is the ticket tally the dashboard has always shown (ADR-0130).
func TestMapKindLoadsVisibleMapsOnly(t *testing.T) {
	k, _ := mapKindFixture(t)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	byID := map[string]work.Container{}
	for _, c := range containers {
		ids = append(ids, c.ID)
		byID[c.ID] = c
	}
	for _, shown := range []string{"2026-07-01-active", "2026-07-02-arrived"} {
		if !slices.Contains(ids, shown) {
			t.Fatalf("missing map container %q; got %v", shown, ids)
		}
	}
	for _, hidden := range []string{"2026-07-03-abandoned", "2026-07-04-archived", "2026-07-05-broken"} {
		if slices.Contains(ids, hidden) {
			t.Fatalf("hidden map %q still present: %v", hidden, ids)
		}
	}

	active := byID["2026-07-01-active"]
	if active.Kind != k.ID() {
		t.Fatalf("container kind = %q, want %q", active.Kind, k.ID())
	}
	if active.Project != "pop" || active.Checkout != "/repo/main" {
		t.Fatalf("container = %+v, want project pop in /repo/main", active)
	}
	if want := "WAYFINDING · 2 open / 1 frontier"; work.StatusCellText(k.StatusCell(active)) != want {
		t.Fatalf("StatusCell = %q, want %q", work.StatusCellText(k.StatusCell(active)), want)
	}
	if active.Worktree != "" {
		t.Fatalf("container = %+v, want a map row with a blank worktree", active)
	}
	if len(active.Items) != 2 {
		t.Fatalf("items = %+v, want the two tickets", active.Items)
	}
	if active.Items[0].Blocked {
		t.Fatalf("frontier ticket %+v reported blocked", active.Items[0])
	}
	if !active.Items[1].Blocked {
		t.Fatalf("ticket behind an open blocker %+v reported unblocked", active.Items[1])
	}
	if len(active.DetailSections) != 1 || active.DetailSections[0].Title != "Destination" {
		t.Fatalf("detail sections = %+v, want the Destination prose", active.DetailSections)
	}
}

// TestMapKindOrdersAndSummarises pins the Map kind's own comparator (project,
// then id descending) and its header phrase. Nothing here ranks a Map against a
// task set — the seam's kind precedence does that, and this comparator never sees
// another kind's container.
func TestMapKindOrdersAndSummarises(t *testing.T) {
	k, _ := mapKindFixture(t)
	snap, err := work.BuildSnapshot([]work.Kind{k})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range snap.Containers {
		ids = append(ids, c.ID)
	}
	want := []string{"2026-07-02-arrived", "2026-07-01-active"}
	if !slices.Equal(ids, want) {
		t.Fatalf("order = %v, want %v (newest Map of a project first)", ids, want)
	}
	if got := snap.SummaryLine(); got != "2 maps" {
		t.Fatalf("summary = %q, want %q", got, "2 maps")
	}
	if len(k.Summary(nil)) != 0 {
		t.Fatalf("Summary of no Maps = %v, want nothing said", k.Summary(nil))
	}
}

// TestMapKindVerbsFollowTheFrontier pins the verb surface: a Map with a frontier
// offers to work it, the two shared verbs are always there, and a ticket offers
// the work verb only while it is actually workable.
func TestMapKindVerbsFollowTheFrontier(t *testing.T) {
	k, _ := mapKindFixture(t)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var active work.Container
	for _, c := range containers {
		if c.ID == "2026-07-01-active" {
			active = c
		}
	}
	verbs := func(actions []work.Action) []work.Verb {
		var out []work.Verb
		for _, a := range actions {
			out = append(out, a.Verb)
		}
		return out
	}
	wantActions := []work.Verb{VerbWork, VerbFanOut, VerbAssist, work.VerbShell, VerbWorkHere, VerbFanOutHere}
	if got := verbs(k.Actions(active)); !slices.Equal(got, wantActions) {
		t.Fatalf("map actions = %v, want %v", got, wantActions)
	}
	if got, want := verbs(k.ItemActions(active, active.Items[0])), []work.Verb{VerbWork, VerbWorkHere, work.VerbCopyName}; !slices.Equal(got, want) {
		t.Fatalf("frontier ticket actions = %v, want %v", got, want)
	}
	if got, want := verbs(k.ItemActions(active, active.Items[1])), []work.Verb{work.VerbCopyName}; !slices.Equal(got, want) {
		t.Fatalf("blocked ticket actions = %v, want %v", got, want)
	}
	// All four frontier keys are gated on a frontier: a Map with none offers no
	// dead key, going or staying. Assist survives the gate — a Map whose frontier
	// is empty or fully claimed is when a Map-scoped session is most needed
	// (ADR-0184). The way off the row is the Status menu, and muting it is the Mute
	// menu; neither is in this list at all, both opening from the row list
	// (ADR-0236 decisions 1 and 5).
	frontierless := active
	frontierless.MapFrontier = 0
	if got, want := verbs(k.Actions(frontierless)), []work.Verb{VerbAssist, work.VerbShell}; !slices.Equal(got, want) {
		t.Fatalf("frontierless map actions = %v, want %v", got, want)
	}

	// copy-name copies the map id for a row and the bare ticket id for a ticket,
	// which is what each surface's paste target has always been.
	out, err := k.Perform(active, nil, work.VerbCopyName)
	if err != nil || out.Clipboard != "2026-07-01-active" {
		t.Fatalf("copy-name on the map = %+v, %v", out, err)
	}
	ticket := active.Items[0]
	out, err = k.Perform(active, &ticket, work.VerbCopyName)
	if err != nil || out.Clipboard != "01" {
		t.Fatalf("copy-name on a ticket = %+v, %v", out, err)
	}
	if _, err := k.Perform(active, nil, work.Verb("drain")); err == nil {
		t.Fatal("a Task-set verb on a Map should be refused")
	}
}

// TestMapCopyMenuOffersNameAndFolder pins the Map's copy menu (ADR-0236 decision
// 6): the name on `n` and the Map's own folder on `y`, so `y` `y` copies the
// directory its map.md and tickets live in. Neither is in Actions any more.
func TestMapCopyMenuOffersNameAndFolder(t *testing.T) {
	k, _ := mapKindFixture(t)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var active work.Container
	for _, c := range containers {
		if c.ID == "2026-07-01-active" {
			active = c
		}
	}
	var keyed []string
	for _, a := range k.CopyActions(active) {
		keyed = append(keyed, a.Key+"="+string(a.Verb))
	}
	if want := []string{"n=copy-name", "y=copy-map-path"}; !slices.Equal(keyed, want) {
		t.Fatalf("map copy menu = %v, want %v", keyed, want)
	}
	for _, a := range k.Actions(active) {
		if a.Verb == work.VerbCopyName || a.Verb == VerbCopyMapPath || a.Verb == work.VerbCopy {
			t.Fatalf("Actions offered %s — the copy menu owns it", a.Verb)
		}
	}
	out, err := k.Perform(active, nil, VerbCopyMapPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/data/repos/repo-aaaa", "maps", "2026-07-01-active"); out.Clipboard != want {
		t.Fatalf("copy-map-path clipboard = %q, want %q", out.Clipboard, want)
	}
}

// TestMapKindVerbCapabilities is the Map's half of the grant list ADR-0254
// decision 5 asks to be reviewable: every verb the kind owns, with the one bit
// that says whether a Selection may run it. The four frontier verbs and assist
// each resolve a session per Map and hand the operator to a pane, so none of
// them is plural; copy-name and the whole status vocabulary are.
func TestMapKindVerbCapabilities(t *testing.T) {
	k, _ := mapKindFixture(t)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var active work.Container
	for _, c := range containers {
		if c.ID == "2026-07-01-active" {
			active = c
		}
	}
	active.MutedUntil = time.Now().Add(time.Hour)
	plural := map[work.Verb]bool{
		work.VerbCopyName: true,
		VerbReopen:        true, VerbAbandon: true, VerbArchive: true, VerbUnarchive: true,
	}
	for _, action := range append(k.Actions(active), k.StatusActions(active)...) {
		if got := action.Modes.AllowsPlural(); got != plural[action.Verb] {
			t.Fatalf("%s plural = %v, want %v", action.Verb, got, plural[action.Verb])
		}
	}
	for _, action := range k.ItemActions(active, active.Items[0]) {
		if action.Modes.AllowsPlural() {
			t.Fatalf("ticket verb %s is plural, want every item verb singular", action.Verb)
		}
	}
}

// TestMapKindWorkVerbOpensTheGrillingPane pins the container-level work verb: it
// runs the same spawn `pop map next` runs — one tagged pane per ticket in the Map
// session's single window — and hands the caller that session to switch to, rather
// than switching on its own (ADR-0158).
func TestMapKindWorkVerbOpensTheGrillingPane(t *testing.T) {
	k, _ := mapKindFixture(t)
	fake := &tmuxtest.Fake{}
	k.d.Wayfinder.Tmux = fake
	k.d.Wayfinder.Trunk = func() (string, error) { return "/repo/trunk", nil }

	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var active work.Container
	for _, c := range containers {
		if c.ID == "2026-07-01-active" {
			active = c
		}
	}
	out, err := k.Perform(active, nil, VerbWork)
	if err != nil {
		t.Fatalf("work verb: %v", err)
	}
	session := MapSessionName(active.ID)
	panes := fake.Windows[session]["map"]
	wantTitle := "01-research · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(nil))
	if len(panes) != 1 || fake.PaneTitles[panes[0]] != wantTitle {
		t.Fatalf("windows = %v (titles %v), want one pane titled %q", fake.Windows[session], fake.PaneTitles, wantTitle)
	}
	// The ticket's own pane, not the session: a Map session tiles every ticket in
	// one window, so a session-named handoff would land the caller wherever that
	// window was last left.
	if out.Kind != work.OutcomeHandoff || out.Handoff.Kind != work.HandoffTmux || out.Handoff.Target != panes[0] {
		t.Fatalf("outcome = %+v, want a tmux handoff to pane %q", out, panes[0])
	}
	if len(fake.Switched) != 0 {
		t.Fatalf("the verb moved the caller itself: %v", fake.Switched)
	}
	if strings.Contains(out.Message, "reclaim") {
		t.Fatalf("working a free ticket reported a reclaim: %q", out.Message)
	}

	// The grilling session ends and tmux hands that pane's id to a new process:
	// the ticket is back on the frontier, and the spawn that takes it over says
	// so on the dashboard's own outcome line, naming the dead owner and when it
	// claimed — the human's only clue that drafts may be lying in the Map folder.
	pane := fake.Windows[session]["map"][0]
	deadOwner := "pane:" + pane + "/" + strconv.Itoa(fake.PanePIDs[pane])
	fake.PaneInfos[pane] = tmuxmod.PaneInfo{Session: session, Command: "zsh"}
	fake.PanePIDs[pane] = fake.PanePIDs[pane] + 1
	again, err := k.Perform(active, nil, VerbWork)
	if err != nil {
		t.Fatalf("work verb after the session died: %v", err)
	}
	if !strings.Contains(again.Message, "reclaimed 01 from dead owner "+deadOwner) {
		t.Fatalf("outcome message = %q, want it to report the reclaim from %s", again.Message, deadOwner)
	}
	if !strings.Contains(again.Message, "(claimed ") {
		t.Fatalf("outcome message = %q, want it to say when the dead owner claimed", again.Message)
	}
	for _, forbidden := range []string{"stole", "stolen", "steal", "expire", "warning"} {
		if strings.Contains(again.Message, forbidden) {
			t.Fatalf("outcome message says %q: %s", forbidden, again.Message)
		}
	}

	// The arrived Map has no frontier, so there is nothing to work and the verb
	// says so instead of opening an empty window.
	for _, c := range containers {
		if c.ID == "2026-07-02-arrived" {
			if _, err := k.Perform(c, nil, VerbWork); err == nil {
				t.Fatal("working a Map with an empty frontier should be refused")
			}
		}
	}
}

// TestMapKindClaimedTicketIsVisitable drives the whole path a live grilling
// session leaves behind: work a ticket, and the row it becomes must lead back to
// the pane doing the work. A claim lives exactly as long as its owner's process
// (ADR-0193), so a claimed ticket is off the frontier and unworkable — before
// this verb its row offered copy-name alone, and the session grilling it was
// reachable from the dashboard only by killing it.
func TestMapKindClaimedTicketIsVisitable(t *testing.T) {
	k, _ := mapKindFixture(t)
	fake := &tmuxtest.Fake{}
	k.d.Wayfinder.Tmux = fake
	k.d.Wayfinder.Trunk = func() (string, error) { return "/repo/trunk", nil }

	activeMap := func() work.Container {
		t.Helper()
		containers, err := k.Load()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range containers {
			if c.ID == "2026-07-01-active" {
				return c
			}
		}
		t.Fatal("the active map went missing")
		return work.Container{}
	}

	if _, err := k.Perform(activeMap(), nil, VerbWork); err != nil {
		t.Fatalf("work verb: %v", err)
	}
	pane := fake.Windows[MapSessionName("2026-07-01-active")]["map"][0]

	active := activeMap()
	ticket := active.Items[0]
	if ticket.ID != "01" || ticket.Status != string(TicketClaimed) {
		t.Fatalf("ticket after working it = %+v, want 01 claimed", ticket)
	}
	verbs := []work.Verb{VerbVisit, work.VerbCopyName}
	var got []work.Verb
	for _, a := range k.ItemActions(active, ticket) {
		got = append(got, a.Verb)
	}
	if !slices.Equal(got, verbs) {
		t.Fatalf("claimed ticket actions = %v, want %v", got, verbs)
	}

	out, err := k.Perform(active, &ticket, VerbVisit)
	if err != nil {
		t.Fatalf("visit verb: %v", err)
	}
	if out.Kind != work.OutcomeHandoff || out.Handoff.Target != pane {
		t.Fatalf("outcome = %+v, want a handoff to the grilling pane %q", out, pane)
	}
	// Visiting is navigation: the pane already grilling the ticket is the whole
	// answer, so nothing is split beside it and nothing is typed into it.
	if panes := fake.Windows[MapSessionName("2026-07-01-active")]["map"]; len(panes) != 1 {
		t.Fatalf("panes after a visit = %v, want the one pane the work verb opened", panes)
	}
	if sent := fake.SentCommands[pane]; len(sent) != 1 {
		t.Fatalf("keys sent to the pane = %v, want only the spawn's own command", sent)
	}

	// The ticket the operator can still work is the one nothing holds, and a
	// resolved or blocked ticket remains a dead row.
	if _, err := k.Perform(active, &active.Items[1], VerbVisit); err == nil {
		t.Fatal("visiting an unclaimed ticket should be refused")
	}
}
