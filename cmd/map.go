package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

var mapStatusAll bool

// mapNextFocus and mapFanOutFocus are the opt-in move. Both spawning verbs leave
// the operator where they were by default: a fan-out is often typed to get agents
// running, not to be taken to one of them (ADR-0182).
var (
	mapNextFocus   bool
	mapFanOutFocus bool
	mapAssistFocus bool
)

var mapCmd = &cobra.Command{
	Use:   "map",
	Short: "Browse and manage Maps",
	Long: `Browse and manage Maps.

Maps live under the Task-storage maps/ directory as plain markdown: map.md plus
issues/ Decision tickets, with index.json holding ticket state. Charting ends
with ` + "`pop map register`" + `, which validates that manifest and makes the
Map registered Work.

Grilling then draws from the frontier: ` + "`pop map next`" + ` claims the first
open, unblocked, unclaimed ticket, grills it in a pane and prints where to read it,
so several panes can grill one Map at once, and ` + "`pop map fan-out`" + ` does that
for every frontier ticket in one act. A claim is a pop.db row owned by the pane
running the agent, never a file state; it frees itself after four hours.

` + "`pop map assist`" + ` is the way in that holds no ticket: a session scoped to the
whole Map, for an idea about the Map's own shape. It claims nothing and resolves
nothing, it is reachable whatever the frontier looks like, and a second call lands
in the first pane rather than racing it on the Map's prose.

` + "`pop map resolve`" + ` closes a ticket: it writes the answer, flips the manifest
entry and re-renders map.md's generated index in one re-runnable call, and
` + "`pop map out-of-scope`" + ` does the same into the Out of scope section. pop is the
only writer of those regions — they carry pop:generated markers and are rebuilt
from the manifest on every resolve, so hand-edits inside them are lost.

` + "`--adr`" + ` and ` + "`--context`" + ` (both repeatable, resolve only) declare
draft files a decision produced; pop never parses the answer for links, so a
draft is verified to exist and recorded on the manifest entry, or the resolve
refuses naming the missing path. A dirty repository working tree only warns —
pop cannot tell an unrelated in-flight change from a stray fragment a grilling
session left behind.

` + "`pop map spawned <map-id> <task-set-id>`" + ` records the handoff: the set id is
appended to the manifest's ` + "`spawned_sets`" + ` and ` + "`## Spawned sets`" + ` is
re-rendered from it. It is idempotent, and it is the only writer of that section —
appending a set there by hand is lost on the next resolve. There is no reverse
flag on ` + "`pop tasks register`" + `; the set's own index.json carries
` + "`source_map`" + ` as the other half of the link. ` + "`pop map status <map-id>`" + ` prints
those sets back with their live status and task tally — the same block the Work
dashboard's detail pane shows, read fresh from the sets and stored nowhere. A set
that resolves to nothing renders ` + "`(missing)`" + ` rather than disappearing.

Every Map gets a tmux session of its own, ` + "`pop-map-<map-id>`" + `, rooted at the
Trunk worktree. It has one window, ` + "`map`" + `, holding one tiled pane per ticket
being grilled — tagged with the ticket id, titled with the ticket file's stem, and
spawned by ` + "`next`" + ` or ` + "`fan-out`" + ` — plus the Map's own ` + "`assist`" + `
pane beside them. There is no overview pane:
` + "`pop map status <map-id>`" + ` is a verb you type. No spawning verb moves you
unless you pass ` + "`--focus`" + `, and a pane whose agent is still alive is a jump
target that is never sent work twice. The other writes auto-open —
` + "`register`" + `, ` + "`claim`" + `, ` + "`resolve`" + `, ` + "`out-of-scope`" + ` and ` + "`spawned`" + ` run
in place, ensure the session exists and report where it is, so a verb called from
a Task-set pane never relocates you. ` + "`status`" + ` creates no
tmux state at all. Pass ` + "`--trunk <path>`" + ` when pop cannot work out the Trunk
on its own.

A Map ends at ` + "`pop map arrive`" + `, which writes ` + "`Status: arrived`" + ` and tears down
the Map's tmux session; ` + "`pop map open`" + ` reverses it, reopening the Map and putting
you back in its session. The gate is
the destination, not empty fog — a Map may carry non-prerequisite fog forever — so
arrival warns about open or claimed tickets and proceeds. An arrived Map stays
visible; ` + "`pop map archive`" + ` is what hides one. A ` + "`Status:`" + ` line outside
` + "`active | arrived | abandoned`" + ` renders the Map BROKEN with the fix printed.`,
}

var mapStatusCmd = &cobra.Command{
	Use:   "status [MAP]",
	Short: "List every map, or show one map's detail",
	Args:  cobra.MaximumNArgs(1),
	Run:   runMapStatus,
}

// mapRegisterCmd deliberately carries no --managed flag: wayfinding writes
// nothing into the repository, so a Map has no checkout of its own and
// provisions no worktree (ADR-0172).
var mapRegisterCmd = &cobra.Command{
	Use:   "register MAP",
	Short: "End charting: validate a map's manifest and register it as Work",
	Long: `End charting: validate a map's manifest and register it as Work.

A MALFORMED map is a fix loop, not a failure — the diagnostics name every
problem at once, so fix what they name and run this again.

For the shape of the files themselves — layout, templates and manifest fields —
run "pop map authoring-guide".`,
	Args: cobra.ExactArgs(1),
	Run:  runMapRegister,
}

// mapNextCmd is the parallel-grilling primitive: several panes run it against the
// same Map and each gets a different ticket, because each claim is one transaction
// taken for the pane that got the ticket. The map argument is optional — a
// repository wayfinding one Map needs no id.
var mapNextCmd = &cobra.Command{
	Use:   "next [MAP]",
	Short: "Claim the first frontier ticket, grill it in a pane, and print where it lives",
	Args:  cobra.MaximumNArgs(1),
	Run:   runMapNext,
}

// mapFanOutCmd is `next` over the whole frontier: one Grilling pane per ticket in
// one act, so a wayfinding sitting is walked in parallel rather than one question
// at a time. It is a loop over the same spawn, so a ticket a parallel session takes
// mid-loop costs one pane and nothing else.
var mapFanOutCmd = &cobra.Command{
	Use:   "fan-out [MAP]",
	Short: "Grill every frontier ticket, one tiled pane each",
	Args:  cobra.MaximumNArgs(1),
	Run:   runMapFanOut,
}

// mapAssistCmd is the way in that holds no ticket: a session scoped to the Map
// itself, for the idea that arrives about the Map's own shape. It is deliberately
// ungated by the frontier — an empty or fully-claimed frontier is when it is most
// needed (ADR-0184).
var mapAssistCmd = &cobra.Command{
	Use:   "assist [MAP]",
	Short: "Open a session scoped to the whole map — no ticket, no claim, no resolve",
	Long: `Open a session scoped to the whole map — no ticket, no claim, no resolve.

An idea about the map itself — new scope for a ticket, a fresh ticket, a patch of
fog, something past the destination — belongs in an assist session rather than in
whichever ticket happened to be open. It claims nothing and resolves nothing:
` + "`pop map resolve`" + ` belongs to the ticket's own claimed session.

One pane per map, reused: a second call lands in the first pane rather than
racing it on the map's prose. The frontier is not consulted, so assist is
reachable when every ticket is resolved, blocked or claimed.

For what an assist session may write, run ` + "`pop map authoring-guide`" + `.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runMapAssist,
}

var mapClaimCmd = &cobra.Command{
	Use:   "claim MAP NN",
	Short: "Claim one named Decision ticket",
	Args:  cobra.ExactArgs(2),
	Run:   runMapClaim,
}

// mapResolveCmd is one atomic write of three files. It is also re-runnable: a
// second run replaces the answer instead of appending one, so a mistake is fixed
// by resolving again rather than by hand-editing what pop generated.
var mapResolveCmd = &cobra.Command{
	Use:   "resolve MAP NN --answer-file PATH",
	Short: "Record a decision: write the answer, resolve the ticket, re-render the index",
	Args:  cobra.ExactArgs(2),
	Run:   runMapResolve,
}

// mapOutOfScopeCmd is the other resolution path. It is a verb rather than a flag
// on resolve because the destination section differs: a scope boundary is not a
// step on the route actually walked.
var mapOutOfScopeCmd = &cobra.Command{
	Use:   "out-of-scope MAP NN --reason WHY",
	Short: "Resolve a ticket by ruling it beyond the destination",
	Args:  cobra.ExactArgs(2),
	Run:   runMapOutOfScope,
}

// mapSpawnedCmd is the only writer of the Map's lineage. `to-tasks` calls it
// after `pop tasks register`, so the set exists by the time the Map names it; the
// id is taken bare and never checked against the Task store, because a Map is a
// historical record of what the effort spawned and a set may later be archived or
// deleted without rewriting that.
var mapSpawnedCmd = &cobra.Command{
	Use:   "spawned MAP SET",
	Short: "Record a task set this map spawned",
	Args:  cobra.ExactArgs(2),
	Run:   runMapSpawned,
}

var (
	mapResolveAnswerFile    string
	mapResolveADRDrafts     []string
	mapResolveContextDrafts []string
	mapOutOfScopeReason     string
	mapTrunk                string
)

// mapArriveCmd is the Map's terminal act, and the one gate that is a judgment
// rather than a count: it warns about unfinished tickets and proceeds.
var mapArriveCmd = &cobra.Command{
	Use:   "arrive MAP",
	Short: "Declare a map's destination reached and tear down its session",
	Args:  cobra.ExactArgs(1),
	Run:   runMapArrive,
}

// mapOpenCmd is both halves of "take me back to this Map": the status write that
// reverses arrival, and the create-or-attach of the Map's tmux session.
var mapOpenCmd = &cobra.Command{
	Use:   "open MAP",
	Short: "Open a map's tmux session, reopening an arrived map",
	Args:  cobra.ExactArgs(1),
	Run:   runMapOpen,
}

var mapArchiveCmd = &cobra.Command{
	Use:   "archive MAP",
	Short: "Hide a map from default views",
	Args:  cobra.ExactArgs(1),
	Run:   runMapArchive,
}

var mapUnarchiveCmd = &cobra.Command{
	Use:   "unarchive MAP",
	Short: "Restore an archived map to default views",
	Args:  cobra.ExactArgs(1),
	Run:   runMapUnarchive,
}

func init() {
	rootCmd.AddCommand(mapCmd)
	mapCmd.AddCommand(mapStatusCmd)
	mapCmd.AddCommand(mapRegisterCmd)
	mapCmd.AddCommand(mapNextCmd)
	mapCmd.AddCommand(mapFanOutCmd)
	mapCmd.AddCommand(mapAssistCmd)
	mapCmd.AddCommand(mapClaimCmd)
	mapCmd.AddCommand(mapResolveCmd)
	mapCmd.AddCommand(mapOutOfScopeCmd)
	mapCmd.AddCommand(mapSpawnedCmd)
	mapCmd.AddCommand(mapArriveCmd)
	mapCmd.AddCommand(mapOpenCmd)
	mapCmd.AddCommand(mapArchiveCmd)
	mapCmd.AddCommand(mapUnarchiveCmd)
	mapStatusCmd.Flags().BoolVar(&mapStatusAll, "all", false, "include abandoned and archived maps")
	mapResolveCmd.Flags().StringVar(&mapResolveAnswerFile, "answer-file", "", "file holding the answer body written under ## Answer")
	_ = mapResolveCmd.MarkFlagRequired("answer-file")
	mapResolveCmd.Flags().StringArrayVar(&mapResolveADRDrafts, "adr", nil, "path to an ADR draft this decision produced; repeat for more than one")
	mapResolveCmd.Flags().StringArrayVar(&mapResolveContextDrafts, "context", nil, "path to a CONTEXT.md glossary draft this decision produced; repeat for more than one")
	mapOutOfScopeCmd.Flags().StringVar(&mapOutOfScopeReason, "reason", "", "why the ticket is beyond the destination")
	_ = mapOutOfScopeCmd.MarkFlagRequired("reason")
	mapNextCmd.Flags().BoolVar(&mapNextFocus, "focus", false, "switch to the map's window after spawning")
	mapFanOutCmd.Flags().BoolVar(&mapFanOutFocus, "focus", false, "switch to the map's window after spawning")
	mapAssistCmd.Flags().BoolVar(&mapAssistFocus, "focus", false, "switch to the map's window after spawning")
	// --trunk goes on the verbs that refuse without a Trunk. The in-place writes
	// only ever warn about the session, and the read verbs create none, so a flag
	// there would advertise a side effect they do not have.
	for _, c := range []*cobra.Command{mapNextCmd, mapFanOutCmd, mapAssistCmd, mapOpenCmd} {
		c.Flags().StringVar(&mapTrunk, "trunk", "", "Trunk worktree to root the map's tmux session at")
	}
}

// mapVerbDeps wires the Trunk resolver onto the wayfinder deps used by the
// session-touching verbs. Resolution is the cmd layer's job: it is the only
// layer that holds both the repository config and the caller's --trunk override.
func mapVerbDeps() *wayfinder.Deps {
	d := cmdLayerDeps().wayfinderDeps()
	if d.Trunk == nil {
		d.Trunk = resolveMapTrunk
	}
	return d
}

func resolveMapTrunk() (string, error) {
	d := cmdLayerDeps()
	checkout, err := d.DirOrGetwd()
	if err != nil {
		return "", err
	}
	return resolveManagedTrunk(d.tasksDeps(), mapVerbConfig(), checkout, mapTrunk)
}

// mapVerbConfig loads the user config for the Map verbs, tolerating its absence.
// A missing or broken config is not a refusal: trunk resolution falls back to the
// repository's main worktree and the agent preset falls back to its default,
// which is the right answer for a repo that configured neither.
func mapVerbConfig() *config.Config {
	d := cmdLayerDeps()
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPathWith(d.configDeps())
	}
	cfg, err := config.LoadWith(d.configDeps(), cfgPath)
	if err != nil {
		return nil
	}
	return cfg
}

// reportMapSession is the auto-open half of the Map verbs' house rule: the write
// has already landed, so a session pop could not open warns rather than fails.
// An agent resolving a ticket from a Task-set pane must not be blocked by the
// state of the human's tmux, and it must not be relocated either — this reports
// where the session is and never switches the caller's client.
func reportMapSession(w io.Writer, d *wayfinder.Deps, mapID string) {
	session, err := wayfinder.EnsureMapSession(d, mapID)
	if err != nil {
		fmt.Fprintf(w, "warning: no map session: %v\n", err)
		return
	}
	if session.Created {
		fmt.Fprintf(w, "opened tmux session %s at %s\n", session.Name, session.Dir)
		return
	}
	fmt.Fprintf(w, "tmux session %s is live\n", session.Name)
}

// runMapStatus is `pop map status [MAP]`: bare, it lists every map; given a map
// id it prints that one map's detail, folding the former `pop map show` verb in
// (ADR-0181's sibling consistency fix — `pop tasks status` already carries both
// halves of this same question).
func runMapStatus(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		err := runMapShowWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
		handleTaskExit(err)
		return
	}
	err := runMapStatusWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, mapStatusAll)
	handleTaskExit(err)
}

func runMapStatusWith(d *wayfinder.Deps, w io.Writer, includeAll bool) error {
	snap, err := wayfinder.BuildStatus(d, cmdLayerDeps().WorkDir(), includeAll)
	if err != nil {
		return err
	}
	return wayfinder.RenderStatus(w, snap)
}

func runMapShowWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	return wayfinder.ShowWith(d, w, cmdLayerDeps().WorkDir(), mapID)
}

func runMapRegister(cmd *cobra.Command, args []string) {
	err := runMapRegisterWith(mapVerbDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapRegisterWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.RegisterMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	if result.AlreadyRegistered {
		fmt.Fprintf(w, "Map %s is already registered\n", result.MapID)
	} else {
		fmt.Fprintf(w, "Registered map %s\n", result.MapID)
	}
	printMapWarnings(w, result.Warnings)
	reportMapSession(w, d, result.MapID)
	return nil
}

func runMapNext(cmd *cobra.Command, args []string) {
	var mapID string
	if len(args) > 0 {
		mapID = args[0]
	}
	err := runMapNextWith(mapVerbDeps(), os.Stdout, mapID, mapNextFocus)
	handleTaskExit(err)
}

// runMapNextWith spawns the ticket's pane and then claims for it, which is why
// nothing here resolves the Trunk first: a Trunk that cannot be named refuses
// inside the spawn, before any claim exists to strand.
func runMapNextWith(d *wayfinder.Deps, w io.Writer, mapID string, focus bool) error {
	out, err := wayfinder.NextFrontierTicket(d, mapVerbConfig(), cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	renderSpawnedTickets(w, out)
	return focusMapSession(d, out, focus)
}

func runMapFanOut(cmd *cobra.Command, args []string) {
	var mapID string
	if len(args) > 0 {
		mapID = args[0]
	}
	err := runMapFanOutWith(mapVerbDeps(), os.Stdout, mapID, mapFanOutFocus)
	handleTaskExit(err)
}

// runMapFanOutWith walks the whole frontier through the same spawn `next` uses. An
// empty frontier is a message and exit 0, not a refusal, so fan-out is safe to
// type speculatively at the top of a sitting.
func runMapFanOutWith(d *wayfinder.Deps, w io.Writer, mapID string, focus bool) error {
	out, err := wayfinder.FanOutFrontier(d, mapVerbConfig(), cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	renderSpawnedTickets(w, out)
	return focusMapSession(d, out, focus)
}

// renderSpawnedTickets prints one claim-shaped block per spawned ticket and then a
// total. The first field of the first line of each block is the ticket id and the
// second its path, exactly as the single-ticket verb has always printed them, so a
// skill parsing `next` parses a fan-out N times over.
func renderSpawnedTickets(w io.Writer, out *wayfinder.FrontierSpawn) {
	for i := range out.Spawned {
		renderClaim(w, out.Spawned[i].Claim)
		renderGrillingPane(w, out.Spawned[i].Pane)
	}
	if len(out.Spawned) == 0 {
		fmt.Fprintf(w, "map %s: no frontier ticket to grill — every Decision ticket is resolved, blocked, or claimed\n", out.MapID)
		return
	}
	fmt.Fprintf(w, "%d of %d frontier tickets grilling in %s\n", len(out.Spawned), out.Frontier, out.Session.Name)
	if out.Lost > 0 {
		fmt.Fprintf(w, "%d went to another session mid-fan-out, leaving an idle pane each\n", out.Lost)
	}
}

// renderGrillingPane names the pane the work is now happening in.
func renderGrillingPane(w io.Writer, pane *wayfinder.GrillingPane) {
	verb := "opened"
	if pane.Reused {
		verb = "returned to"
	}
	fmt.Fprintf(w, "%s grilling pane %s in %s:%s\n", verb, pane.Title, pane.Session.Name, pane.Window)
}

// focusMapSession moves the operator only when asked, and only when there is a
// session to move to.
func focusMapSession(d *wayfinder.Deps, out *wayfinder.FrontierSpawn, focus bool) error {
	if !focus || len(out.Spawned) == 0 {
		return nil
	}
	return wayfinder.FocusMapSession(d, out.Session)
}

func runMapAssist(cmd *cobra.Command, args []string) {
	var mapID string
	if len(args) > 0 {
		mapID = args[0]
	}
	err := runMapAssistWith(mapVerbDeps(), os.Stdout, mapID, mapAssistFocus)
	handleTaskExit(err)
}

// runMapAssistWith opens the Map's own session. It prints the write boundary
// alongside the pane, because the one rule an unclaimed, unscoped session has to
// hold is the one it never sees enforced: resolving belongs to the ticket's own
// claimed session.
func runMapAssistWith(d *wayfinder.Deps, w io.Writer, mapID string, focus bool) error {
	pane, err := wayfinder.AssistMap(d, mapVerbConfig(), cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	verb := "opened"
	if pane.Reused {
		verb = "returned to"
	}
	fmt.Fprintf(w, "%s assist pane %s in %s:%s\n", verb, pane.Title, pane.Session.Name, pane.Window)
	fmt.Fprintf(w, "map %s: scoped to the whole map — no ticket claimed, and this session resolves none\n", pane.MapID)
	if !focus {
		return nil
	}
	return wayfinder.FocusMapSession(d, pane.Session)
}

func runMapClaim(cmd *cobra.Command, args []string) {
	err := runMapClaimWith(mapVerbDeps(), os.Stdout, args[0], args[1])
	handleTaskExit(err)
}

func runMapClaimWith(d *wayfinder.Deps, w io.Writer, mapID, ticket string) error {
	result, err := wayfinder.ClaimTicket(d, cmdLayerDeps().WorkDir(), mapID, ticket)
	if err != nil {
		return err
	}
	renderClaim(w, result)
	reportMapSession(w, d, result.MapID)
	return nil
}

// renderClaim leads with the two facts a grilling session acts on — the ticket id
// and the path to its markdown — on one tab-separated line, so a skill can read
// them without parsing prose. Everything a human needs follows underneath.
func renderClaim(w io.Writer, result *wayfinder.ClaimResult) {
	fmt.Fprintf(w, "%s\t%s\n", result.Ticket.ID, result.Path)
	fmt.Fprintf(w, "map %s, claimed by %s\n", result.MapID, result.Owner)
	if result.Stole != nil {
		fmt.Fprintf(w, "stole an expired claim held by %s since %s\n",
			result.Stole.Owner, result.Stole.ClaimedAt.Format(time.RFC3339))
	}
	if len(result.UnresolvedBlockers) > 0 {
		fmt.Fprintf(w, "warning: blocked by %s, still unresolved\n",
			strings.Join(result.UnresolvedBlockers, ", "))
	}
}

func runMapResolve(cmd *cobra.Command, args []string) {
	err := runMapResolveWith(mapVerbDeps(), os.Stdout, wayfinder.ResolveRequest{
		MapID:         args[0],
		Ticket:        args[1],
		AnswerFile:    mapResolveAnswerFile,
		ADRDrafts:     mapResolveADRDrafts,
		ContextDrafts: mapResolveContextDrafts,
	})
	handleTaskExit(err)
}

func runMapResolveWith(d *wayfinder.Deps, w io.Writer, req wayfinder.ResolveRequest) error {
	result, err := wayfinder.ResolveTicket(d, cmdLayerDeps().WorkDir(), req)
	if err != nil {
		return err
	}
	renderResolution(w, result)
	reportMapSession(w, d, result.MapID)
	return nil
}

func runMapOutOfScope(cmd *cobra.Command, args []string) {
	err := runMapOutOfScopeWith(mapVerbDeps(), os.Stdout, wayfinder.ResolveRequest{
		MapID:  args[0],
		Ticket: args[1],
		Reason: mapOutOfScopeReason,
	})
	handleTaskExit(err)
}

func runMapOutOfScopeWith(d *wayfinder.Deps, w io.Writer, req wayfinder.ResolveRequest) error {
	result, err := wayfinder.RuleOutOfScope(d, cmdLayerDeps().WorkDir(), req)
	if err != nil {
		return err
	}
	renderResolution(w, result)
	reportMapSession(w, d, result.MapID)
	return nil
}

// renderResolution leads with the ticket and its path, as `next` and `claim` do,
// then names the generated section the decision landed in — the file a session
// must not hand-edit is the one it should go read.
func renderResolution(w io.Writer, result *wayfinder.ResolveResult) {
	fmt.Fprintf(w, "%s\t%s\n", result.Ticket.ID, result.Path)
	section := "Decisions so far"
	if result.OutOfScope {
		section = "Out of scope"
	}
	fmt.Fprintf(w, "resolved in map %s, rendered into %q\n", result.MapID, section)
	if result.Replaced {
		fmt.Fprintln(w, "replaced the answer a previous resolve wrote")
	}
	if result.ReleasedClaim != "" {
		fmt.Fprintf(w, "released the claim held by %s\n", result.ReleasedClaim)
	}
	if result.DirtyRepo {
		fmt.Fprintln(w, "warning: the repository working tree is dirty")
	}
	printMapWarnings(w, result.Warnings)
}

// printMapWarnings prints a Map's advisory manifest problems in the one shape
// every verb says them. Advisory throughout: the verb has already done its work
// by the time these are printed.
func printMapWarnings(w io.Writer, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
}

func runMapSpawned(cmd *cobra.Command, args []string) {
	err := runMapSpawnedWith(mapVerbDeps(), os.Stdout, args[0], args[1])
	handleTaskExit(err)
}

func runMapSpawnedWith(d *wayfinder.Deps, w io.Writer, mapID, setID string) error {
	result, err := wayfinder.RecordSpawnedSet(d, cmdLayerDeps().WorkDir(), mapID, setID)
	if err != nil {
		return err
	}
	if result.AlreadyRecorded {
		fmt.Fprintf(w, "map %s already lists task set %s\n", result.MapID, result.SetID)
	} else {
		fmt.Fprintf(w, "map %s spawned task set %s\n", result.MapID, result.SetID)
	}
	fmt.Fprintf(w, "%d spawned set(s): %s\n", len(result.SpawnedSets), strings.Join(result.SpawnedSets, ", "))
	reportMapSession(w, d, result.MapID)
	return nil
}

func runMapArrive(cmd *cobra.Command, args []string) {
	err := runMapArriveWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapArriveWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.ArriveMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	renderArrival(w, result)
	return nil
}

func runMapOpen(cmd *cobra.Command, args []string) {
	err := runMapOpenWith(mapVerbDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapOpenWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.OpenMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	renderArrival(w, result)
	return nil
}

// renderArrival names the new status first, then everything the declaration did
// not stop for: the tickets left unfinished (a warning, never a refusal) and the
// session that went with the Map.
func renderArrival(w io.Writer, result *wayfinder.ArrivalResult) {
	if result.Unchanged {
		fmt.Fprintf(w, "Map %s is already %s\n", result.MapID, result.Status)
	} else {
		fmt.Fprintf(w, "Map %s is %s (was %s)\n", result.MapID, result.Status, result.Previous)
	}
	if len(result.Unfinished) > 0 {
		fmt.Fprintf(w, "warning: %d ticket(s) still unresolved:\n", len(result.Unfinished))
		for _, t := range result.Unfinished {
			line := fmt.Sprintf("  %s  %s", t.ID, t.Status)
			if t.ClaimOwner != "" {
				line += fmt.Sprintf("  (claimed by %s)", t.ClaimOwner)
			}
			fmt.Fprintln(w, line)
		}
	}
	printMapWarnings(w, result.Warnings)
	if result.KilledSession != "" {
		fmt.Fprintf(w, "tore down tmux session %s\n", result.KilledSession)
	}
	if result.Session != nil {
		if result.Session.Created {
			fmt.Fprintf(w, "opened tmux session %s at %s\n", result.Session.Name, result.Session.Dir)
		} else {
			fmt.Fprintf(w, "attached tmux session %s\n", result.Session.Name)
		}
	}
}

func runMapArchive(cmd *cobra.Command, args []string) {
	err := runMapArchiveWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapArchiveWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.ArchiveMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Archived map %s\n", result.MapID)
	return nil
}

func runMapUnarchive(cmd *cobra.Command, args []string) {
	err := runMapUnarchiveWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapUnarchiveWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.UnarchiveMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Unarchived map %s\n", result.MapID)
	return nil
}
