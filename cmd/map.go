package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

var mapStatusAll bool

var mapCmd = &cobra.Command{
	Use:   "map",
	Short: "Browse and manage Maps",
	Long: `Browse and manage Maps.

Maps live under the Task-storage maps/ directory as plain markdown: map.md plus
issues/ Decision tickets, with index.json holding ticket state. Charting ends
with ` + "`pop map register`" + `, which validates that manifest and makes the
Map registered Work.

Grilling then draws from the frontier: ` + "`pop map next`" + ` claims the first
open, unblocked, unclaimed ticket and prints where to read it, so several windows
can grill one Map at once. A claim is a pop.db row owned by the tmux pane (else
the pid) that took it, never a file state; it frees itself after four hours.

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

A Map ends at ` + "`pop map arrive`" + `, which writes ` + "`Status: arrived`" + ` and tears down
the Map's tmux session; ` + "`pop map open`" + ` reverses it when fog reopens. The gate is
the destination, not empty fog — a Map may carry non-prerequisite fog forever — so
arrival warns about open or claimed tickets and proceeds. An arrived Map stays
visible; ` + "`pop map archive`" + ` is what hides one. A ` + "`Status:`" + ` line outside
` + "`active | arrived | abandoned`" + ` renders the Map BROKEN with the fix printed.`,
}

var mapStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show map status",
	Args:  cobra.NoArgs,
	Run:   runMapStatus,
}

var mapShowCmd = &cobra.Command{
	Use:   "show MAP",
	Short: "Show one map in detail",
	Args:  cobra.ExactArgs(1),
	Run:   runMapShow,
}

// mapRegisterCmd deliberately carries no --managed flag: wayfinding writes
// nothing into the repository, so a Map has no checkout of its own and
// provisions no worktree (ADR-0172).
var mapRegisterCmd = &cobra.Command{
	Use:   "register MAP",
	Short: "End charting: validate a map's manifest and register it as Work",
	Args:  cobra.ExactArgs(1),
	Run:   runMapRegister,
}

// mapNextCmd is the parallel-grilling primitive: several windows run it against
// the same Map and each gets a different ticket, because the pick and the claim
// are one transaction. The map argument is optional — a repository wayfinding one
// Map needs no id.
var mapNextCmd = &cobra.Command{
	Use:   "next [MAP]",
	Short: "Claim the first frontier ticket and print where it lives",
	Args:  cobra.MaximumNArgs(1),
	Run:   runMapNext,
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

var (
	mapResolveAnswerFile    string
	mapResolveADRDrafts     []string
	mapResolveContextDrafts []string
	mapOutOfScopeReason     string
)

// mapArriveCmd is the Map's terminal act, and the one gate that is a judgment
// rather than a count: it warns about unfinished tickets and proceeds.
var mapArriveCmd = &cobra.Command{
	Use:   "arrive MAP",
	Short: "Declare a map's destination reached and tear down its session",
	Args:  cobra.ExactArgs(1),
	Run:   runMapArrive,
}

var mapOpenCmd = &cobra.Command{
	Use:   "open MAP",
	Short: "Reopen an arrived map for more wayfinding",
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
	mapCmd.AddCommand(mapShowCmd)
	mapCmd.AddCommand(mapRegisterCmd)
	mapCmd.AddCommand(mapNextCmd)
	mapCmd.AddCommand(mapClaimCmd)
	mapCmd.AddCommand(mapResolveCmd)
	mapCmd.AddCommand(mapOutOfScopeCmd)
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
}

func runMapStatus(cmd *cobra.Command, args []string) {
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

func runMapShow(cmd *cobra.Command, args []string) {
	err := runMapShowWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapShowWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	return wayfinder.ShowWith(d, w, cmdLayerDeps().WorkDir(), mapID)
}

func runMapRegister(cmd *cobra.Command, args []string) {
	err := runMapRegisterWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapRegisterWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.RegisterMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	if result.AlreadyRegistered {
		fmt.Fprintf(w, "Map %s is already registered\n", result.MapID)
		return nil
	}
	fmt.Fprintf(w, "Registered map %s\n", result.MapID)
	return nil
}

func runMapNext(cmd *cobra.Command, args []string) {
	var mapID string
	if len(args) > 0 {
		mapID = args[0]
	}
	err := runMapNextWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, mapID)
	handleTaskExit(err)
}

func runMapNextWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.NextTicket(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	renderClaim(w, result)
	return nil
}

func runMapClaim(cmd *cobra.Command, args []string) {
	err := runMapClaimWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0], args[1])
	handleTaskExit(err)
}

func runMapClaimWith(d *wayfinder.Deps, w io.Writer, mapID, ticket string) error {
	result, err := wayfinder.ClaimTicket(d, cmdLayerDeps().WorkDir(), mapID, ticket)
	if err != nil {
		return err
	}
	renderClaim(w, result)
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
	err := runMapResolveWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, wayfinder.ResolveRequest{
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
	return nil
}

func runMapOutOfScope(cmd *cobra.Command, args []string) {
	err := runMapOutOfScopeWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, wayfinder.ResolveRequest{
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
	err := runMapOpenWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
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
	if result.KilledSession != "" {
		fmt.Fprintf(w, "tore down tmux session %s\n", result.KilledSession)
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
