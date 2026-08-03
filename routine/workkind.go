package routine

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Routine Work kind: the adapter that makes a Routine comply with
// `work.Kind`, wearing the advance seam it already had (advance.go). Both halves
// are one object because they are one kind's two relationships with the Work
// model — what a reader sees, and what the daemon may fire — and a caller that
// wires the kind once should not have to wire it twice.
//
// Membership here is *discovered*, not registered: the Routines that exist are
// whatever the data-dir walk finds, and each one belongs to the canonical cwd its
// state.json was stamped with at creation (BoundDirectory). That is the model rule
// this kind establishes — membership is registered *or* discovered, per kind — and
// the reason a Routine has no `work_containers` row: the row would be a second
// source of a fact the bound directory already holds, would cost a migration of
// every Routine, and would kill Routines bound to directories no repository owns.

// Relevance tiers order a page of Routines by how much a reader standing in one
// checkout is likely to care. They are kind-local: `work` carries the number and
// never interprets it.
const (
	// TierHere is the checkout the reader stands in — Project routines included,
	// since they are committed to that checkout.
	TierHere = 1
	// TierProject is another checkout of the same project.
	TierProject = 2
	// TierElsewhere is everything else, a Routine bound to a directory no
	// configured project covers included.
	TierElsewhere = 3
)

// MissingDirectoryCell is the DIRECTORY cell of a Routine whose bound directory
// is gone. The Routine still lists — pruning it would delete authored work nobody
// asked to delete — and it resolves no Checkout, so a verb refuses instead of
// landing somewhere arbitrary.
const MissingDirectoryCell = "(missing)"

// BrokenStatus is the kind-local status label of a Routine whose definition could
// not be read. It is a status rather than a warning on a side channel: a Routine
// that will not load is the one a reader most needs to see.
const BrokenStatus = "BROKEN"

// brokenSectionTitle heads the detail block carrying an unloadable Routine's
// parse error — the whole of what fixing it needs.
const brokenSectionTitle = "Load error"

// unknownProject labels a container whose bound directory names no project, which
// on this kind means a Routine whose definition would not load at all.
const unknownProject = "unknown"

// KindDeps holds what the Routine kind reads. The Routine deps carry the kind's
// own side (filesystem, store handle, clock, tmux); Checkout and Project are the
// reader's resolved location, which is what relevance tiers are stamped from.
type KindDeps struct {
	Routine *Deps
	// Out receives the advance seam's own narration (a healed-run count). Nil
	// discards, which is what a read-only surface wants.
	Out io.Writer

	// Checkout is the canonical checkout the reader stands in — the tier-1 anchor,
	// and the checkout Project routines are discovered from. Wired by the caller
	// that already resolved it, so neither a dashboard rebuild nor `work` has to
	// ask where "here" is. Empty leaves the kind to resolve the cwd's own checkout.
	Checkout string
	// Project is Checkout's project label, the tier-2 key. Empty is resolved from
	// Checkout through the checkout list below.
	Project string
	// Checkouts lists every configured checkout with the project label it carries —
	// the directory→project map a bound directory's membership is read through.
	// Defaults to the config's picker projects.
	Checkouts func() ([]project.ExpandedProject, error)
}

// Kind is the Routine Work kind: the read seam over the advance seam it embeds.
type Kind struct {
	*Advancer
	kd *KindDeps
}

// NewKind returns the Routine kind over kd. A nil kd resolves to real
// dependencies and an unresolved reader location, which lands every Routine in
// the outermost tier.
func NewKind(kd *KindDeps) *Kind {
	if kd == nil {
		kd = &KindDeps{}
	}
	if kd.Routine == nil {
		kd.Routine = defaultDeps
	}
	return &Kind{Advancer: NewAdvancer(kd.Routine, kd.Out), kd: kd}
}

// Load reads every discovered Routine as a Work container, Project routines
// included. It is a pure read: the run reconcile is the advance seam's phase, so
// rendering a Routine writes nothing, and a machine that has never fired one
// never materialises a database (ADR-0140).
//
// It has no warnings channel and no all-or-nothing error for a bad file: a
// Routine whose definition will not load comes back as a container carrying
// BROKEN and its parse error, because failing the call would blank a whole page
// over one unreadable Routine. The error return is for a directory-level failure,
// as it has always been.
func (k *Kind) Load() ([]work.Container, error) {
	d := k.kd.Routine
	root, _ := k.projectRoutineRoot()
	entries, s, err := loadRoutineEntries(d, root)
	if err != nil {
		return nil, err
	}
	tiers := k.tierTable()
	containers := make([]work.Container, 0, len(entries))
	for _, e := range entries {
		c, err := containerFor(d, e, tiers, s)
		if err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}
	return containers, nil
}

// Less orders two Routines by relevance tier, then by id. It consults no cwd —
// a comparator is pure over two containers — which is exactly why the tier is
// stamped onto the container at load time, where the reader's location is known.
func (k *Kind) Less(a, b work.Container) bool {
	if a.RoutineTier != b.RoutineTier {
		return a.RoutineTier < b.RoutineTier
	}
	return a.ID < b.ID
}

// StatusCell composes a Routine's STATUS cell: its one status label and nothing
// beside it. A Routine's pause reason is already folded into that label
// (`paused (changed)`), and an unloadable Routine's reason belongs in the detail
// sections rather than in a cell a column has to fit.
//
// The tone carries what the label means so a renderer needs no table of Routine
// status words: a finished or live run reads good, a pause reads as wanting
// attention, a failure or an unreadable definition reads bad, and a Routine that
// has simply never fired carries no weight at all.
func (k *Kind) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: c.Status, Tone: statusTone(c.Status)}}
}

// statusTone maps one Routine status label onto its attention level.
func statusTone(status string) work.StatusTone {
	switch {
	case status == "running", status == store.RoutineRunSucceeded:
		return work.ToneGood
	case status == store.RoutineRunFailed, status == BrokenStatus:
		return work.ToneBad
	case strings.HasPrefix(status, pausedStatusPrefix):
		return work.ToneWarn
	}
	return work.ToneLabel
}

// Columns are the headers a page of Routines reads under — the Routine table's
// own five, which share no cell with the Task-set columns.
func (k *Kind) Columns() []string {
	return routineColumns()
}

// routineColumns is the one declaration of the Routine column headers, read both
// by the kind and by the Routine table that renders them.
func routineColumns() []string {
	return []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN", "STATUS"}
}

// Actions returns the container-level verbs that apply to a Routine right now.
// Only the two shared verbs are here: the Routine's own verbs — fire, pause,
// resume, edit, handoff, copy-report-path — become kind-local actions with the
// detail view, and until then they stay where their surface already dispatches
// them.
func (k *Kind) Actions(c work.Container) []work.Action {
	return []work.Action{
		{Verb: work.VerbShell, Key: "O", Label: "shell"},
		{Verb: work.VerbCopyName, Key: "y", Label: "copy name"},
	}
}

// ItemActions returns the verbs for one run. A run is a record, not a thing to
// act on: copying its name is the whole of it until the run verbs land.
func (k *Kind) ItemActions(c work.Container, item work.Item) []work.Action {
	return []work.Action{{Verb: work.VerbCopyName, Key: "y", Label: "copy name"}}
}

// Perform runs one Routine verb.
func (k *Kind) Perform(c work.Container, item *work.Item, verb work.Verb) (work.Outcome, error) {
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
			return work.Outcome{}, fmt.Errorf("routine %q has no directory to open a shell in", c.ID)
		}
		return work.Outcome{
			Kind:    work.OutcomeHandoff,
			Handoff: work.Handoff{Kind: work.HandoffTmux, Dir: dir},
			Message: "shell in " + dir,
		}, nil
	default:
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
}

// Summary returns the Routine kind's header phrases: how many Routines there are,
// how many of them belong to the checkout the reader stands in, and how many are
// paused. The "here" tally is what keeps an unfiltered global list from
// misreporting how much of it belongs to what the reader is looking at.
func (k *Kind) Summary(containers []work.Container) []string {
	here, paused := 0, 0
	for _, c := range containers {
		if c.RoutineTier == TierHere {
			here++
		}
		if c.RoutinePaused {
			paused++
		}
	}
	phrases := []string{work.CountPhrase(len(containers), "routine", "routines")}
	if here > 0 {
		phrases = append(phrases, fmt.Sprintf("%d here", here))
	}
	if paused > 0 {
		phrases = append(phrases, work.CountPhrase(paused, "paused", "paused"))
	}
	return phrases
}

// projectRoutineRoot is the checkout Project routines are discovered from: the
// one the caller wired when it resolved the reader's location, else the cwd's own.
// Preferring the wired one keeps a rebuild from forking git to answer a question
// its caller already answered.
func (k *Kind) projectRoutineRoot() (string, bool) {
	if dir := strings.TrimSpace(k.kd.Checkout); dir != "" {
		return dir, true
	}
	return checkoutRoot(k.kd.Routine)
}

// routineEntry is one discovered Routine with the volatile facts a read surface
// shows, derived once per read. It is deliberately neither projection: the Work
// container carries render-ready cells (a labelled schedule, `(missing)`), while
// the Routine table keeps the raw values its modals prefill from — so both project
// from this rather than one deriving from the other.
type routineEntry struct {
	id string
	// directory is the bound directory as stamped at creation, empty for a Routine
	// whose definition would not load.
	directory string
	// schedule is the raw recurrence expression, empty for a manual-only Routine
	// (ADR-0134) and for every Project routine.
	schedule string
	paused   bool
	// projectRoutine marks a Project routine (ADR-0138): discovered live from a
	// checkout's committed `.pop/routines/`, carrying no pause bit and no schedule.
	projectRoutine bool
	// storeID is the execution-store routine_id this Routine's runs key on, and
	// runsDir the directory its reports land in. For an authored Routine the store
	// id is its own; a Project routine's is synthetic per checkout.
	storeID string
	runsDir string
	lastRun string
	status  string
	// lastReportPath is the newest run's report, empty when the Routine has never
	// fired or its newest run produced none (skipped, or still running).
	lastReportPath string
	// loadErr is the failure that kept this Routine's definition from being read.
	loadErr error
}

// loadRoutineEntries derives every discovered Routine's read-surface facts in one
// pass: the data-dir walk, then the Project routines committed to root (empty root
// ⇒ none). It writes nothing. The borrowed store handle is returned so a caller
// that wants run history reuses this pass's handle rather than opening a second
// one; it is nil on a machine that has fired nothing (ADR-0140).
func loadRoutineEntries(d *Deps, root string) ([]routineEntry, *store.Store, error) {
	routines, warnings, err := ListRoutines(d)
	if err != nil {
		return nil, nil, err
	}
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		s = nil
	}

	entries := make([]routineEntry, 0, len(routines)+len(warnings))
	for _, r := range routines {
		e := routineEntry{
			id:        r.ID,
			directory: r.Manifest.BoundDirectory,
			schedule:  r.Manifest.Schedule,
			paused:    r.Manifest.Paused,
			storeID:   r.ID,
			runsDir:   filepath.Join(routineDir(d, r.ID), runsDirName),
			lastRun:   "never",
			status:    dashboardIdleStatus(r.Manifest, ""),
		}
		if s != nil {
			last, err := LastFireTime(s, r.ID)
			if err != nil {
				return nil, nil, err
			}
			e.lastRun = formatLastRun(last)
			e.status, e.lastReportPath = dashboardStatusFor(d, s, r.ID, r.Manifest)
		}
		entries = append(entries, e)
	}
	// An unreadable Routine takes its place among the readable ones instead of
	// becoming a warning line beside them: it is discovered, it is a Routine, and
	// what it needs is to be seen.
	for _, w := range warnings {
		entries = append(entries, brokenEntry(w.ID, w.Err))
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].id < entries[j].id })

	if strings.TrimSpace(root) != "" {
		projectEntries, err := projectRoutineEntries(d, root, s)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, projectEntries...)
	}
	return entries, s, nil
}

// projectRoutineEntries derives one checkout's Project routines (ADR-0138). Their
// run state is per checkout, so the store lookups key on the synthetic id rather
// than the name, and their idle status derives from an empty manifest — a Project
// routine can never read as paused because it carries no pause bit.
func projectRoutineEntries(d *Deps, root string, s *store.Store) ([]routineEntry, error) {
	routines, warnings := DiscoverProjectRoutinesIn(d, root)
	key := checkoutKey(root)
	loaded := make(map[string]bool, len(routines))
	entries := make([]routineEntry, 0, len(routines)+len(warnings))
	for _, pr := range routines {
		loaded[ProjectOrigin+pr.Name] = true
		storeID := projectStoreID(key, pr.Name)
		e := routineEntry{
			id:             ProjectOrigin + pr.Name,
			directory:      pr.Dir,
			projectRoutine: true,
			storeID:        storeID,
			runsDir:        filepath.Join(projectRoutineDataDir(d, key, pr.Name), runsDirName),
			lastRun:        "never",
			status:         dashboardIdleStatus(Manifest{}, ""),
		}
		if s != nil {
			last, err := LastFireTime(s, storeID)
			if err != nil {
				return nil, err
			}
			e.lastRun = formatLastRun(last)
			e.status, e.lastReportPath = dashboardStatusFor(d, s, storeID, Manifest{})
		}
		entries = append(entries, e)
	}
	// A per-file problem that yielded no routine is a broken entry; one that rode
	// along with a loaded routine (an ignored frontmatter key) is not, or the same
	// Project routine would list twice.
	for _, w := range warnings {
		if loaded[w.ID] {
			continue
		}
		e := brokenEntry(w.ID, w.Err)
		e.projectRoutine = true
		entries = append(entries, e)
	}
	return entries, nil
}

// brokenEntry is the entry an unloadable Routine leaves behind: an id, the
// failure, and nothing else — every other cell is a fact its definition would
// have carried.
func brokenEntry(id string, err error) routineEntry {
	return routineEntry{id: id, storeID: id, loadErr: err, status: BrokenStatus}
}

// containerFor projects one entry onto a Work container, stamping the relevance
// tier the reader's location decides. A broken Routine stops at its status and its
// error: reading runs, a directory or a schedule off a definition that would not
// parse would be inventing them.
func containerFor(d *Deps, e routineEntry, tiers tierTable, s *store.Store) (work.Container, error) {
	cell, checkout := directoryCells(d, e.directory)
	tier, label := tiers.resolve(e, cell == MissingDirectoryCell)
	c := work.Container{
		Kind:            ref.KindRoutine,
		ID:              e.id,
		Project:         label,
		Status:          e.status,
		CursorKey:       "routine\x00" + e.id,
		RoutineSchedule: ScheduleLabel(e.schedule),
		RoutineLastRun:  e.lastRun,
		RoutinePaused:   e.paused,
		RoutineTier:     tier,
	}
	if e.projectRoutine {
		c.Badge = projectRoutineBadge
	}
	if e.loadErr != nil {
		c.Broken = true
		c.BrokenReason = e.loadErr.Error()
		c.Status = BrokenStatus
		c.RoutineSchedule = ""
		c.RoutineLastRun = ""
		c.DetailSections = []work.Section{{Title: brokenSectionTitle, Body: e.loadErr.Error()}}
		return c, nil
	}
	c.RoutineDirectory, c.Checkout = cell, checkout
	items, err := runItems(s, e)
	if err != nil {
		return work.Container{}, err
	}
	c.Items = items
	return c, nil
}

// directoryCells splits a bound directory into the cell a reader sees and the
// path a verb may act on. A directory that is gone still lists, but resolves no
// checkout: a shell or a fire refuses by name rather than landing nowhere.
func directoryCells(d *Deps, dir string) (cell, checkout string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", ""
	}
	if _, err := d.FS.Stat(dir); err != nil {
		return MissingDirectoryCell, ""
	}
	return dir, dir
}

// runItems projects a Routine's run history onto Work items, newest first — the
// order the store lists them in and the order a reader reads them. A run's file
// is the report it wrote, falling back to the path the report would have taken,
// so an item always points somewhere its reader can open without knowing this
// package's layout.
func runItems(s *store.Store, e routineEntry) ([]work.Item, error) {
	if s == nil {
		return nil, nil
	}
	runs, err := s.ListRoutineRuns(e.storeID)
	if err != nil {
		return nil, fmt.Errorf("list routine runs: %w", err)
	}
	items := make([]work.Item, 0, len(runs))
	for _, run := range runs {
		report := run.ReportPath
		if report == "" {
			report = reportPathForRun(e.runsDir, run.FiredAt)
		}
		items = append(items, work.Item{
			ID:          run.FiredAt.UTC().Format("2006-01-02T15:04:05Z"),
			Title:       formatLastRun(run.FiredAt),
			Status:      run.Outcome,
			StatusLabel: runStatusLabel(run),
			File:        report,
		})
	}
	return items, nil
}

// runStatusLabel is the display embellishment a run's outcome carries beyond its
// word: the reason a run was skipped or failed, which is the whole of why the
// outcome reads as it does.
func runStatusLabel(run store.RoutineRun) string {
	reason := run.SkipReason
	if reason == "" {
		reason = run.FailReason
	}
	if reason == "" {
		return ""
	}
	return run.Outcome + " (" + reason + ")"
}

// tierTable stamps relevance tiers and project labels from the reader's own
// location. It is resolved once per Load because the tier is a fact about where
// the reader stands, and Less — pure over two containers — could never derive it.
type tierTable struct {
	// checkout is the reader's canonical checkout, empty when unresolved: then
	// nothing is tier 1 but a Project routine.
	checkout string
	// project is checkout's project label, the tier-2 key.
	project string
	// checkouts is the directory→project map, one entry per configured checkout.
	checkouts []project.ExpandedProject
}

func (k *Kind) tierTable() tierTable {
	t := tierTable{
		checkout:  strings.TrimSpace(k.kd.Checkout),
		project:   strings.TrimSpace(k.kd.Project),
		checkouts: k.checkouts(),
	}
	if t.project == "" && t.checkout != "" {
		t.project = t.labelFor(t.checkout)
	}
	return t
}

// resolve stamps one entry's tier and the project label its container carries. A
// Routine whose bound directory is gone lands in the outermost tier whatever the
// path says: nobody is standing in a directory that no longer exists, and its
// membership can no longer be verified.
func (t tierTable) resolve(e routineEntry, missing bool) (int, string) {
	if e.projectRoutine {
		// A Project routine is committed to the checkout it was discovered in, and
		// discovery only ever reads the checkout the reader stands in — so it is
		// always tier 1, and never a candidate the daemon could fire.
		return TierHere, t.labelOrBase(e.directory)
	}
	dir := strings.TrimSpace(e.directory)
	if dir == "" {
		return TierElsewhere, unknownProject
	}
	label := t.labelOrBase(dir)
	if missing {
		return TierElsewhere, label
	}
	switch {
	case t.checkout != "" && pathWithinOrEqual(dir, t.checkout):
		return TierHere, label
	case t.project != "" && label == t.project:
		return TierProject, label
	}
	return TierElsewhere, label
}

// labelOrBase names the project a directory belongs to, falling back to the
// directory's own last segment. The fallback is what keeps a Routine bound outside
// every configured project labelled rather than blank — it belongs to a directory,
// and that is what the label says.
func (t tierTable) labelOrBase(dir string) string {
	if label := t.labelFor(dir); label != "" {
		return label
	}
	if dir = strings.TrimSpace(dir); dir != "" {
		return filepath.Base(dir)
	}
	return unknownProject
}

// labelFor resolves a directory's project label through the checkout list, taking
// the longest matching checkout so a worktree nested under another checkout is
// read as its own.
func (t tierTable) labelFor(dir string) string {
	best, label := "", ""
	for _, c := range t.checkouts {
		if c.Path == "" || !pathWithinOrEqual(dir, c.Path) {
			continue
		}
		if len(c.Path) > len(best) {
			best, label = c.Path, c.ProjectLabel
		}
	}
	return label
}

// checkouts resolves the directory→project map, through the wired seam when the
// caller supplied one. A resolution that fails degrades the tiers — every Routine
// reads as elsewhere — rather than failing the read: a page of Routines is worth
// more than a page of nothing.
func (k *Kind) checkouts() []project.ExpandedProject {
	if k.kd.Checkouts != nil {
		list, err := k.kd.Checkouts()
		if err != nil {
			return nil
		}
		return list
	}
	d := k.kd.Routine
	if d.LoadConfig == nil {
		return nil
	}
	cfg, err := d.LoadConfig()
	if err != nil {
		return nil
	}
	list, err := tasks.ListPickerProjectsWith(projectDeps(d), cfg)
	if err != nil {
		return nil
	}
	return list
}

// pathWithinOrEqual reports whether p is base or nests under it.
func pathWithinOrEqual(p, base string) bool {
	p, base = filepath.Clean(p), filepath.Clean(base)
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}
