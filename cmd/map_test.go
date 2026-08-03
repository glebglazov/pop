package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

func TestMapCommandTree(t *testing.T) {
	t.Parallel()
	for _, path := range [][]string{
		{"map", "status"},
		{"map", "show"},
		{"map", "register"},
		{"map", "next"},
		{"map", "claim"},
		{"map", "resolve"},
		{"map", "out-of-scope"},
		{"map", "spawned"},
		{"map", "arrive"},
		{"map", "open"},
		{"map", "archive"},
		{"map", "unarchive"},
	} {
		if _, _, err := rootCmd.Find(path); err != nil {
			t.Fatalf("Find(%v): %v", path, err)
		}
	}
	// The rename is a hard cut: cobra resolves an unknown first argument to the
	// root command, so the old family is gone exactly when nothing named
	// wayfinder answers.
	if cmd, _, _ := rootCmd.Find([]string{"wayfinder", "status"}); cmd.CommandPath() != "pop" {
		t.Fatalf("pop wayfinder should not exist; Find resolved %q", cmd.CommandPath())
	}
	for _, cmd := range []*cobra.Command{mapCmd, mapStatusCmd, mapShowCmd, mapRegisterCmd, mapNextCmd, mapClaimCmd, mapResolveCmd, mapOutOfScopeCmd, mapSpawnedCmd, mapArriveCmd, mapOpenCmd, mapArchiveCmd, mapUnarchiveCmd} {
		if strings.Contains(cmd.CommandPath(), "wayfinder") {
			t.Fatalf("command path still says wayfinder: %q", cmd.CommandPath())
		}
	}
}

func TestMapShowRendersMap(t *testing.T) {
	t.Parallel()
	// A real, writable data dir: the legacy header Map below has no manifest, so
	// showing it folds one, and that fold registers the Map in pop.db — a
	// real-disk-only store that a fake "/data" home cannot open.
	dataHome := filepath.Join(t.TempDir(), "xdg")
	commonDir := "/repo/.git"
	setCmdLayerDeps(t, newTestCmdDeps(t, "/mock/cwd", dataHome, ""))
	fs := cmdTestFS(dataHome, "")
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: fs}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "maps", "demo")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"):                 "Status: active\n\n## Destination\nShip it\n\n## Decisions so far\n- one decision",
		filepath.Join(mapDir, "issues", "01-first.md"):  "Type: research\nStatus: resolved\n",
		filepath.Join(mapDir, "issues", "02-second.md"): "Type: task\nBlocked by: 01\n",
	}
	d := wayfinderTestDepsForCmd(t, dataHome, commonDir, files)

	var buf bytes.Buffer
	if err := runMapShowWith(d, &buf, "demo"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Destination: Ship it", "Frontier:", "02-second", "Resolved:", "01-first"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestMapRegisterValidatesThenRegisters walks the charting-to-registered path the
// way a session does: a malformed manifest comes back as a fix list and no row,
// the fix registers, and a re-run is a no-op.
func TestMapRegisterValidatesThenRegisters(t *testing.T) {
	t.Parallel()
	d, storageDir, dataHome := mapRegistryTestDeps(t, map[string]string{
		"maps/2026-08-03-demo/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"maps/2026-08-03-demo/issues/01-first.md": "## Question\nWhy?\n",
		"maps/2026-08-03-demo/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","type":"grilling","status":"parked","blocked_by":["09"]}` +
			`],"spawned_sets":[]}`,
	})

	err := runMapRegisterWith(d, &bytes.Buffer{}, "2026-08-03-demo")
	if err == nil {
		t.Fatal("expected a malformed manifest to refuse registration")
	}
	for _, want := range []string{"MALFORMED", `unknown status "parked"`, `unresolved blocker "09"`, "pop map register 2026-08-03-demo"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, err.Error())
		}
	}

	manifest := filepath.Join(storageDir, "maps", "2026-08-03-demo", "index.json")
	fixed := `{"tickets":[{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]}],"spawned_sets":[]}`
	if err := os.WriteFile(manifest, []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runMapRegisterWith(d, &buf, "2026-08-03-demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Registered map 2026-08-03-demo") {
		t.Fatalf("register output = %q", buf.String())
	}
	if !registeredMap(t, d, "2026-08-03-demo") {
		t.Fatal("register wrote no work_containers row")
	}

	var again bytes.Buffer
	if err := runMapRegisterWith(d, &again, "2026-08-03-demo"); err != nil {
		t.Fatalf("register must be re-runnable: %v", err)
	}
	if !strings.Contains(again.String(), "already registered") {
		t.Fatalf("re-register output = %q", again.String())
	}

	// Plain, never managed: wayfinding writes nothing into the repository, so
	// registration provisions no checkout and the verb has no flag to ask for one.
	if mapRegisterCmd.Flags().Lookup("managed") != nil || mapRegisterCmd.HasAvailableFlags() {
		t.Fatalf("pop map register grew flags: %v", mapRegisterCmd.Flags().FlagUsages())
	}
	assertNoWorktreesProvisioned(t, dataHome)
}

// TestMapNextAndClaimDriveParallelGrilling walks the CLI surface two grilling
// windows share: `next` hands each of them a different frontier ticket and
// prints where to read it, the exhausted frontier is an error, and `claim` is the
// override that still refuses a ticket someone else is holding.
func TestMapNextAndClaimDriveParallelGrilling(t *testing.T) {
	t.Parallel()
	d, storageDir, _ := mapRegistryTestDeps(t, threeTicketMapFiles("demo"))
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}
	nine := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	d.Clock = func() time.Time { return nine }
	d.Owner = func() string { return "pane:%1" }

	var first bytes.Buffer
	if err := runMapNextWith(d, &first, "demo"); err != nil {
		t.Fatalf("next: %v", err)
	}
	wantPath := filepath.Join(storageDir, "maps", "demo", "issues", "01-first.md")
	if got := strings.SplitN(first.String(), "\n", 2)[0]; got != "01\t"+wantPath {
		t.Fatalf("next headline = %q, want the id and path", got)
	}
	if !strings.Contains(first.String(), "claimed by pane:%1") {
		t.Fatalf("next output = %q", first.String())
	}

	d.Owner = func() string { return "pane:%2" }
	var second bytes.Buffer
	if err := runMapNextWith(d, &second, "demo"); err != nil {
		t.Fatalf("second next: %v", err)
	}
	if !strings.HasPrefix(second.String(), "03\t") {
		t.Fatalf("second window got %q, want ticket 03 (02 is blocked)", second.String())
	}

	if err := runMapNextWith(d, &bytes.Buffer{}, "demo"); err == nil {
		t.Fatal("expected an exhausted frontier to fail")
	} else if !strings.Contains(err.Error(), "frontier is empty") {
		t.Fatalf("empty-frontier error = %v", err)
	}

	if err := runMapClaimWith(d, &bytes.Buffer{}, "demo", "01"); err == nil {
		t.Fatal("expected claim to refuse a ticket held by another window")
	} else if !strings.Contains(err.Error(), "pane:%1") {
		t.Fatalf("claim refusal = %v", err)
	}

	// Four hours on, the first window is gone and its ticket comes back — with
	// the steal on the record.
	d.Clock = func() time.Time { return nine.Add(5 * time.Hour) }
	var stolen bytes.Buffer
	if err := runMapNextWith(d, &stolen, "demo"); err != nil {
		t.Fatalf("next after the TTL: %v", err)
	}
	if !strings.HasPrefix(stolen.String(), "01\t") || !strings.Contains(stolen.String(), "stole an expired claim held by pane:%1") {
		t.Fatalf("steal output = %q", stolen.String())
	}

	// `pop map show` is where a human sees who holds what; the files never say.
	var shown bytes.Buffer
	if err := runMapShowWith(d, &shown, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), "claimed by pane:%2") {
		t.Fatalf("show output does not report the live claim:\n%s", shown.String())
	}
	manifest, err := os.ReadFile(filepath.Join(storageDir, "maps", "demo", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "claim") {
		t.Fatalf("the manifest records a claim:\n%s", manifest)
	}
}

// TestMapResolveAndOutOfScopeCloseTickets walks the resolution surface: `resolve`
// takes the answer from a file and reports where it landed, `out-of-scope` closes
// into the other section, and a Map that never had the generated sections gains
// them.
func TestMapResolveAndOutOfScopeCloseTickets(t *testing.T) {
	t.Parallel()
	d, storageDir, _ := mapRegistryTestDeps(t, threeTicketMapFiles("demo"))
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}
	d.Clock = func() time.Time { return time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC) }
	d.Owner = func() string { return "pane:%1" }
	if err := runMapClaimWith(d, &bytes.Buffer{}, "demo", "01"); err != nil {
		t.Fatal(err)
	}

	answerPath := filepath.Join(t.TempDir(), "answer.md")
	if err := os.WriteFile(answerPath, []byte("Postgres, because the data is relational.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var resolved bytes.Buffer
	err := runMapResolveWith(d, &resolved, wayfinder.ResolveRequest{MapID: "demo", Ticket: "01", AnswerFile: answerPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	ticketPath := filepath.Join(storageDir, "maps", "demo", "issues", "01-first.md")
	if got := strings.SplitN(resolved.String(), "\n", 2)[0]; got != "01\t"+ticketPath {
		t.Fatalf("resolve headline = %q, want the id and path", got)
	}
	for _, want := range []string{`rendered into "Decisions so far"`, "released the claim held by pane:%1"} {
		if !strings.Contains(resolved.String(), want) {
			t.Fatalf("resolve output missing %q:\n%s", want, resolved.String())
		}
	}

	var ruledOut bytes.Buffer
	err = runMapOutOfScopeWith(d, &ruledOut, wayfinder.ResolveRequest{
		MapID: "demo", Ticket: "03", Reason: "A separate effort owns the client.",
	})
	if err != nil {
		t.Fatalf("out-of-scope: %v", err)
	}
	if !strings.Contains(ruledOut.String(), `rendered into "Out of scope"`) {
		t.Fatalf("out-of-scope output = %q", ruledOut.String())
	}

	mapMD, err := os.ReadFile(filepath.Join(storageDir, "maps", "demo", "map.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Decisions so far",
		"<!-- pop:generated decisions -->",
		"- [01-first](issues/01-first.md) — Postgres, because the data is relational.",
		"## Out of scope",
		"- [03-third](issues/03-third.md) — A separate effort owns the client.",
		"## Spawned sets",
	} {
		if !strings.Contains(string(mapMD), want) {
			t.Fatalf("map.md missing %q:\n%s", want, mapMD)
		}
	}

	// Both resolutions move the frontier on: 01 is gone from it and 02, which
	// waited on 01, is what the next window is handed.
	var next bytes.Buffer
	if err := runMapNextWith(d, &next, "demo"); err != nil {
		t.Fatalf("next after resolving: %v", err)
	}
	if !strings.HasPrefix(next.String(), "02\t") {
		t.Fatalf("next handed out %q, want the newly unblocked 02", next.String())
	}
}

// TestMapResolveDraftFlagsRecordAndWarnOnDirtyTree drives the CLI-facing pieces
// slice 09 adds: a declared draft is verified and recorded on the manifest, a
// missing one refuses by name, and a dirty repository only ever warns.
func TestMapResolveDraftFlagsRecordAndWarnOnDirtyTree(t *testing.T) {
	t.Parallel()
	files := oneTicketMapFiles("demo")
	files["maps/demo/adrs/978d65fd-slug.md"] = "# Decision\n\nShip it.\n"
	d, storageDir, _ := mapRegistryTestDeps(t, files)
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(storageDir, "maps", "demo")

	answerPath := filepath.Join(t.TempDir(), "answer.md")
	if err := os.WriteFile(answerPath, []byte("Ship it.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// mapRegistryTestDeps' Git mock ignores its arguments and always answers with
	// the git-common-dir path, so `status --porcelain` reads as non-empty: dirty
	// by default. That is exactly the case this resolve should warn on, not
	// refuse.
	var resolved bytes.Buffer
	err := runMapResolveWith(d, &resolved, wayfinder.ResolveRequest{
		MapID: "demo", Ticket: "01", AnswerFile: answerPath,
		ADRDrafts: []string{"adrs/978d65fd-slug.md"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(resolved.String(), "warning: the repository working tree is dirty") {
		t.Fatalf("resolve output missing the dirty-tree warning:\n%s", resolved.String())
	}

	manifest, err := wayfinder.LoadMapManifest(d, mapDir)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Valid || len(manifest.Tickets) == 0 || len(manifest.Tickets[0].ADRDrafts) != 1 ||
		manifest.Tickets[0].ADRDrafts[0] != "adrs/978d65fd-slug.md" {
		t.Fatalf("manifest does not record the declared draft: %+v", manifest.Tickets)
	}
	before, err := os.ReadFile(filepath.Join(mapDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	// A declared draft that does not exist refuses by name, writing nothing.
	err = runMapResolveWith(d, &bytes.Buffer{}, wayfinder.ResolveRequest{
		MapID: "demo", Ticket: "01", AnswerFile: answerPath,
		ADRDrafts: []string{"adrs/nope.md"},
	})
	if err == nil || !strings.Contains(err.Error(), "--adr adrs/nope.md") {
		t.Fatalf("resolve with a missing draft = %v, want a refusal naming it", err)
	}
	after, err := os.ReadFile(filepath.Join(mapDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("a refused resolve modified the manifest:\nbefore: %s\nafter: %s", before, after)
	}
}

// TestMapSpawnedRecordsTheHandoff walks the CLI half of the lineage link: the
// verb reports what it recorded, a second call over the same set is an
// idempotent no-op, and the generated `## Spawned sets` section is what a reader
// of map.md sees.
func TestMapSpawnedRecordsTheHandoff(t *testing.T) {
	t.Parallel()
	d, storageDir, _ := mapRegistryTestDeps(t, oneTicketMapFiles("demo"))
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}

	var recorded bytes.Buffer
	if err := runMapSpawnedWith(d, &recorded, "demo", "2026-08-05-implementing"); err != nil {
		t.Fatalf("spawned: %v", err)
	}
	if !strings.Contains(recorded.String(), "map demo spawned task set 2026-08-05-implementing") {
		t.Fatalf("spawned output = %q", recorded.String())
	}

	var again bytes.Buffer
	if err := runMapSpawnedWith(d, &again, "demo", "2026-08-05-implementing"); err != nil {
		t.Fatalf("second spawned: %v", err)
	}
	if !strings.Contains(again.String(), "already lists task set 2026-08-05-implementing") {
		t.Fatalf("second spawned output = %q", again.String())
	}

	mapDir := filepath.Join(storageDir, "maps", "demo")
	manifest, err := os.ReadFile(filepath.Join(mapDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(manifest), "2026-08-05-implementing"); got != 1 {
		t.Fatalf("manifest lists the set %d times:\n%s", got, manifest)
	}
	mapMD, err := os.ReadFile(filepath.Join(mapDir, "map.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Spawned sets",
		"<!-- pop:generated spawned-sets -->",
		"- 2026-08-05-implementing",
	} {
		if !strings.Contains(string(mapMD), want) {
			t.Fatalf("map.md missing %q:\n%s", want, mapMD)
		}
	}

	// `pop map show` prints the lineage block with the set's status read fresh. No
	// such set exists in this fixture's storage, and the id still renders: the Map
	// records what the effort spawned, so a set that resolves to nothing is
	// reported, never dropped.
	var shown bytes.Buffer
	if err := runMapShowWith(d, &shown, "demo"); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(shown.String(), "Spawned sets:\n  2026-08-05-implementing — (missing)") {
		t.Fatalf("show output missing the lineage block:\n%s", shown.String())
	}
}

func TestMapClaimCompletionOffersUnresolvedTickets(t *testing.T) {
	t.Parallel()
	d, _, _ := mapRegistryTestDeps(t, threeTicketMapFiles("demo"))
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}
	if ids, _ := mapClaimCmd.ValidArgsFunction(mapClaimCmd, nil, ""); !slices.Equal(ids, []string{"demo"}) {
		t.Fatalf("first positional completion = %v, want [demo]", ids)
	}
	ids, _ := mapClaimCmd.ValidArgsFunction(mapClaimCmd, []string{"demo"}, "")
	if !slices.Equal(ids, []string{"01", "02", "03"}) {
		t.Fatalf("ticket completion = %v", ids)
	}
	if third, _ := mapClaimCmd.ValidArgsFunction(mapClaimCmd, []string{"demo", "01"}, ""); third != nil {
		t.Fatalf("completion offered a third positional: %v", third)
	}
}

// TestMapSessionPerMapAutoOpensWithoutRelocatingTheCaller walks the session
// contract from the CLI: `next` spawns a grilling window and moves you there,
// the in-place writes ensure the session and merely say where it is, and the
// read verbs touch tmux not at all.
func TestMapSessionPerMapAutoOpensWithoutRelocatingTheCaller(t *testing.T) {
	t.Parallel()
	d, _, _ := mapRegistryTestDeps(t, threeTicketMapFiles("demo"))
	fake := d.Tmux.(*tmuxtest.Fake)
	fake.Inside = true
	session := wayfinder.MapSessionName("demo")

	var registered bytes.Buffer
	if err := runMapRegisterWith(d, &registered, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(registered.String(), "opened tmux session "+session) {
		t.Fatalf("register did not report the session it opened:\n%s", registered.String())
	}
	if stamp := fake.WorkStamps[session]; stamp.Kind != "map" || stamp.ID != "demo" {
		t.Fatalf("work stamp = %+v, want kind map with the map id", stamp)
	}
	if len(fake.Switched) != 0 || len(fake.Attached) != 0 {
		t.Fatalf("register relocated the caller: switched=%v attached=%v", fake.Switched, fake.Attached)
	}

	var next bytes.Buffer
	if err := runMapNextWith(d, &next, "demo"); err != nil {
		t.Fatalf("next: %v", err)
	}
	if !strings.Contains(next.String(), "opened grilling window "+session+":01-first") {
		t.Fatalf("next did not report its window:\n%s", next.String())
	}
	panes := fake.Windows[session]["01-first"]
	if len(panes) != 1 {
		t.Fatalf("grilling windows = %v, want one named after the ticket", fake.Windows[session])
	}
	if got := strings.Join(fake.SentCommands[panes[0]], " "); !strings.Contains(got, "/pop-wayfinder work demo 01") {
		t.Fatalf("grilling window runs %q, want the work-mode invocation", got)
	}
	if len(fake.Switched) != 1 || fake.Switched[0] != panes[0] {
		t.Fatalf("next did not switch to the grilling window: %v", fake.Switched)
	}

	// An in-place write from anywhere reports the session and leaves the caller
	// where they are — an agent resolving from a Task-set pane must not be moved.
	var claimed bytes.Buffer
	if err := runMapClaimWith(d, &claimed, "demo", "03"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !strings.Contains(claimed.String(), "tmux session "+session+" is live") {
		t.Fatalf("claim did not report the live session:\n%s", claimed.String())
	}
	answerPath := filepath.Join(t.TempDir(), "answer.md")
	if err := os.WriteFile(answerPath, []byte("Because it is simpler.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var resolved bytes.Buffer
	if err := runMapResolveWith(d, &resolved, wayfinder.ResolveRequest{
		MapID: "demo", Ticket: "01", AnswerFile: answerPath,
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var ruledOut bytes.Buffer
	if err := runMapOutOfScopeWith(d, &ruledOut, wayfinder.ResolveRequest{
		MapID: "demo", Ticket: "03", Reason: "Another effort owns it.",
	}); err != nil {
		t.Fatalf("out-of-scope: %v", err)
	}
	for name, out := range map[string]string{"resolve": resolved.String(), "out-of-scope": ruledOut.String()} {
		if !strings.Contains(out, "tmux session "+session+" is live") {
			t.Fatalf("%s did not report the live session:\n%s", name, out)
		}
	}
	if len(fake.Switched) != 1 || len(fake.Attached) != 0 {
		t.Fatalf("an in-place write relocated the caller: switched=%v attached=%v", fake.Switched, fake.Attached)
	}

	// Reads never create tmux state, so they get a fake with nothing arranged.
	quiet := &tmuxtest.Fake{}
	d.Tmux = quiet
	if err := runMapShowWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}
	if err := runMapStatusWith(d, &bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	if len(quiet.Live) != 0 || len(quiet.Windows) != 0 || len(quiet.SentCommands) != 0 ||
		len(quiet.Switched) != 0 || len(quiet.Attached) != 0 || len(quiet.WorkStamps) != 0 {
		t.Fatalf("a read verb touched tmux: %+v", quiet)
	}
}

// An unresolvable Trunk refuses before anything is claimed, and names the flag
// that fixes it.
func TestMapNextRefusesAnUnresolvableTrunk(t *testing.T) {
	t.Parallel()
	d, _, _ := mapRegistryTestDeps(t, threeTicketMapFiles("demo"))
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}
	d.Trunk = func() (string, error) { return "", wayfinder.ErrNoTrunk }

	err := runMapNextWith(d, &bytes.Buffer{}, "demo")
	if err == nil || !strings.Contains(err.Error(), "--trunk <path>") {
		t.Fatalf("err = %v, want a refusal naming --trunk <path>", err)
	}
	// The frontier is untouched, so the ticket is still there once a Trunk is named.
	d.Trunk = func() (string, error) { return t.TempDir(), nil }
	var next bytes.Buffer
	if err := runMapNextWith(d, &next, "demo"); err != nil {
		t.Fatalf("next after naming a trunk: %v", err)
	}
	if !strings.HasPrefix(next.String(), "01\t") {
		t.Fatalf("next handed out %q, want the first ticket still unclaimed", next.String())
	}
	if mapNextCmd.Flags().Lookup("trunk") == nil || mapOpenCmd.Flags().Lookup("trunk") == nil {
		t.Fatal("the verbs that refuse without a Trunk must offer --trunk")
	}
}

// TestMapArriveAndOpenDeclareArrival walks the terminal state from the CLI: the
// declaration warns about what is unfinished instead of refusing, the session goes
// with the Map, the arrived Map stays on the table, and open puts it back.
func TestMapArriveAndOpenDeclareArrival(t *testing.T) {
	t.Parallel()
	d, storageDir, _ := mapRegistryTestDeps(t, oneTicketMapFiles("demo"))
	fake := &tmuxtest.Fake{Live: map[string]string{wayfinder.MapSessionName("demo"): "/repo"}}
	d.Tmux = fake
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}

	var arriveBuf bytes.Buffer
	if err := runMapArriveWith(d, &arriveBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	out := arriveBuf.String()
	for _, want := range []string{
		"Map demo is arrived (was active)",
		"warning: 1 ticket(s) still unresolved",
		"01  open",
		"tore down tmux session " + wayfinder.MapSessionName("demo"),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("arrive output missing %q:\n%s", want, out)
		}
	}
	if fake.HasSession(wayfinder.MapSessionName("demo")) {
		t.Fatal("the map's tmux session survived arrival")
	}
	body, err := os.ReadFile(filepath.Join(storageDir, "maps", "demo", "map.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Status: arrived") {
		t.Fatalf("map.md missing the arrived status:\n%s", body)
	}

	var statusBuf bytes.Buffer
	if err := runMapStatusWith(d, &statusBuf, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusBuf.String(), "demo") || !strings.Contains(statusBuf.String(), "arrived") {
		t.Fatalf("arrived map missing from the default table:\n%s", statusBuf.String())
	}

	var openBuf bytes.Buffer
	if err := runMapOpenWith(d, &openBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(openBuf.String(), "Map demo is active (was arrived)") {
		t.Fatalf("open output = %q", openBuf.String())
	}
}

func TestMapArchiveRoundTrip(t *testing.T) {
	t.Parallel()
	d, storageDir, _ := mapRegistryTestDeps(t, oneTicketMapFiles("demo"))
	mapPath := filepath.Join(storageDir, "maps", "demo", "map.md")
	original, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := runMapArchiveWith(d, &bytes.Buffer{}, "demo"); err == nil {
		t.Fatal("expected archive to refuse an unregistered map")
	} else if !strings.Contains(err.Error(), "pop map register demo") {
		t.Fatalf("error = %v", err)
	}
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}

	var archiveBuf bytes.Buffer
	if err := runMapArchiveWith(d, &archiveBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archiveBuf.String(), "Archived map demo") {
		t.Fatalf("archive output = %q", archiveBuf.String())
	}
	if after, err := os.ReadFile(mapPath); err != nil || string(after) != string(original) {
		t.Fatalf("archive mutated map.md (%v)", err)
	}

	var statusBuf bytes.Buffer
	if err := runMapStatusWith(d, &statusBuf, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statusBuf.String(), "demo") {
		t.Fatalf("archived map visible in default status:\n%s", statusBuf.String())
	}

	var unarchiveBuf bytes.Buffer
	if err := runMapUnarchiveWith(d, &unarchiveBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unarchiveBuf.String(), "Unarchived map demo") {
		t.Fatalf("unarchive output = %q", unarchiveBuf.String())
	}
}

// TestMapShellCompletionOffersMapIDs pins the completion split: every verb
// offers the visible Maps, unarchive offers only the filed-away one.
func TestMapShellCompletionOffersMapIDs(t *testing.T) {
	t.Parallel()
	files := oneTicketMapFiles("visible")
	for rel, content := range oneTicketMapFiles("filed-away") {
		files[rel] = content
	}
	d, _, _ := mapRegistryTestDeps(t, files)
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "filed-away"); err != nil {
		t.Fatal(err)
	}
	if err := runMapArchiveWith(d, &bytes.Buffer{}, "filed-away"); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []*cobra.Command{mapShowCmd, mapRegisterCmd, mapArchiveCmd} {
		got, directive := cmd.ValidArgsFunction(cmd, nil, "")
		if !slices.Equal(got, []string{"visible"}) {
			t.Fatalf("%s completion = %v, want [visible]", cmd.Name(), got)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("%s completion directive = %v", cmd.Name(), directive)
		}
	}
	got, _ := mapUnarchiveCmd.ValidArgsFunction(mapUnarchiveCmd, nil, "")
	if !slices.Equal(got, []string{"filed-away"}) {
		t.Fatalf("unarchive completion = %v, want [filed-away]", got)
	}
	if second, _ := mapShowCmd.ValidArgsFunction(mapShowCmd, []string{"visible"}, ""); second != nil {
		t.Fatalf("completion offered a second positional: %v", second)
	}
}

func TestMapShowUnknownMap(t *testing.T) {
	t.Parallel()
	setCmdLayerDeps(t, newTestCmdDeps(t, "/mock/cwd", "/data", ""))
	d := wayfinderTestDepsForCmd(t, "/data", "/repo/.git", nil)
	err := runMapShowWith(d, &bytes.Buffer{}, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown wayfinder map") {
		t.Fatalf("error = %v", err)
	}
}

func TestMapStatusOutsideGitRepo(t *testing.T) {
	t.Parallel()
	d := &wayfinder.Deps{
		FS: deps.NewRealFileSystem(),
		Tasks: &tasks.Deps{
			FS: deps.NewRealFileSystem(),
			Git: &deps.MockGit{
				CommandInDirFunc: func(dir string, args ...string) (string, error) {
					return "", errNotGit
				},
			},
		},
	}
	err := runMapStatusWith(d, &bytes.Buffer{}, false)
	if err == nil {
		t.Fatal("expected error outside git repository")
	}
}

var errNotGit = errString("fatal: not a git repository")

type errString string

func (e errString) Error() string { return string(e) }

func TestMapStatusEmpty(t *testing.T) {
	t.Parallel()
	setCmdLayerDeps(t, newTestCmdDeps(t, "/mock/cwd", "/data", ""))
	d := wayfinderTestDepsForCmd(t, "/data", "/repo/.git", nil)
	var buf bytes.Buffer
	if err := runMapStatusWith(d, &buf, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No wayfinder maps.") {
		t.Fatalf("output = %q", buf.String())
	}
}

// mapRegistryTestDeps wires cmd-layer deps at a real temp data dir. Registration
// and archival are rows in pop.db, which cannot ride the filesystem seam, so the
// verbs that write them need a real store. Keys in files are relative to the
// repository's Task-storage root; it returns that root and the data dir.
func mapRegistryTestDeps(t *testing.T, files map[string]string) (*wayfinder.Deps, string, string) {
	t.Helper()
	root := t.TempDir()
	dataHome := filepath.Join(root, "xdg")
	commonDir := filepath.Join(root, "repo", ".git")
	fs := cmdTestFS(dataHome, "")
	td := &tasks.Deps{
		FS: fs,
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) { return commonDir, nil },
		},
	}
	t.Cleanup(func() { _ = td.CloseStore() })
	trunk := filepath.Join(root, "repo")
	wd := &wayfinder.Deps{
		FS:    fs,
		Tasks: td,
		Tmux:  &tmuxtest.Fake{},
		Trunk: func() (string, error) { return trunk, nil },
		Exe:   func() (string, error) { return "/bin/pop", nil },
	}
	setCmdLayerDeps(t, &Deps{Dir: trunk, FS: fs, Tasks: td, Wayfinder: wd})

	id, err := tasks.IdentityFromCommonDir(td, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(id.StorageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return wd, id.StorageDir, dataHome
}

// oneTicketMapFiles is the smallest Map that registers cleanly.
func oneTicketMapFiles(id string) map[string]string {
	return map[string]string{
		"maps/" + id + "/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"maps/" + id + "/issues/01-first.md": "## Question\nWhy?\n",
		"maps/" + id + "/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]}` +
			`],"spawned_sets":[]}`,
	}
}

// threeTicketMapFiles is a Map with a frontier of two: 02 waits on 01.
func threeTicketMapFiles(id string) map[string]string {
	return map[string]string{
		"maps/" + id + "/map.md":              "Status: active\n\n## Destination\nShip it\n",
		"maps/" + id + "/issues/01-first.md":  "## Question\nFirst?\n",
		"maps/" + id + "/issues/02-second.md": "## Question\nSecond?\n",
		"maps/" + id + "/issues/03-third.md":  "## Question\nThird?\n",
		"maps/" + id + "/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]},` +
			`{"id":"02","file":"02-second.md","type":"grilling","status":"open","blocked_by":["01"]},` +
			`{"id":"03","file":"03-third.md","type":"grilling","status":"open","blocked_by":[]}` +
			`],"spawned_sets":[]}`,
	}
}

func registeredMap(t *testing.T, d *wayfinder.Deps, mapID string) bool {
	t.Helper()
	s, _, err := d.Tasks.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := s.FindWorkContainer(wayfinder.MapRef(mapID))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func assertNoWorktreesProvisioned(t *testing.T, dataHome string) {
	t.Helper()
	err := filepath.WalkDir(dataHome, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "worktrees" {
			t.Fatalf("registering a map provisioned a worktree root at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func wayfinderTestDepsForCmd(t *testing.T, dataHome, commonDir string, files map[string]string) *wayfinder.Deps {
	t.Helper()
	fs := &deps.MockFileSystem{
		GetwdFunc: func() (string, error) { return "/mock/cwd", nil },
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return "/mock/home", nil },
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			entries := dirEntriesForCmd(path, files)
			if entries == nil {
				return nil, os.ErrNotExist
			}
			return entries, nil
		},
		ReadFileFunc: func(path string) ([]byte, error) {
			if content, ok := files[path]; ok {
				return []byte(content), nil
			}
			return nil, os.ErrNotExist
		},
	}
	return &wayfinder.Deps{
		FS: fs,
		Tasks: &tasks.Deps{
			FS: fs,
			Git: &deps.MockGit{
				CommandInDirFunc: func(dir string, args ...string) (string, error) {
					return commonDir, nil
				},
			},
		},
	}
}

func dirEntriesForCmd(path string, files map[string]string) []os.DirEntry {
	children := map[string]bool{}
	dirs := map[string]bool{}
	for filePath := range files {
		if !strings.HasPrefix(filePath, path+string(os.PathSeparator)) && filePath != path {
			continue
		}
		rel := strings.TrimPrefix(filePath, path+string(os.PathSeparator))
		if rel == "" {
			continue
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		name := parts[0]
		if len(parts) == 1 {
			children[name] = false
			continue
		}
		children[name] = true
		dirs[name] = true
	}
	if len(children) == 0 {
		return nil
	}
	var out []os.DirEntry
	for name, isDir := range children {
		out = append(out, deps.MockDirEntry{NameVal: name, IsDirVal: isDir || dirs[name]})
	}
	return out
}
