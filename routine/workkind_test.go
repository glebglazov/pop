package routine

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// kindOver wires the Routine kind the way a read surface does: the reader's
// resolved location, and the checkout list membership is read through.
func kindOver(d *Deps, checkout, projectLabel string, checkouts []project.ExpandedProject) *Kind {
	return NewKind(&KindDeps{
		Routine:   d,
		Out:       io.Discard,
		Checkout:  checkout,
		Project:   projectLabel,
		Checkouts: func() ([]project.ExpandedProject, error) { return checkouts, nil },
	})
}

func containerByID(t *testing.T, containers []work.Container, id string) work.Container {
	t.Helper()
	for _, c := range containers {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no container %q among %+v", id, containers)
	return work.Container{}
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}


// TestRoutineKindStampsTiersAtLoadAndOrdersByThem drives the whole relevance
// model through the real snapshot builder: the tier is a fact about where the
// reader stands, so it is stamped when the container is built, and the comparator
// only reads what is stamped. The ids are deliberately alphabetically opposite to
// the tiers, so an order that came from the ids alone could not pass.
func TestRoutineKindStampsTiersAtLoadAndOrdersByThem(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	here := filepath.Join(root, "main")
	sibling := filepath.Join(root, "feature-wt")
	elsewhere := filepath.Join(root, "unrelated")
	mkdirs(t, here, sibling, elsewhere)
	d := routineDeps(t, dataHome)
	for id, cwd := range map[string]string{
		"zzz-here":      here,
		"mmm-sibling":   sibling,
		"aaa-elsewhere": elsewhere,
	} {
		if _, err := AddWith(d, id, "daily at 10:00", cwd); err != nil {
			t.Fatal(err)
		}
	}

	k := kindOver(d, canonical(t, here), "pop", []project.ExpandedProject{
		{Name: "pop", ProjectLabel: "pop", Path: canonical(t, here)},
		{Name: "pop/feature", ProjectLabel: "pop", Path: canonical(t, sibling)},
		{Name: "other", ProjectLabel: "other", Path: canonical(t, elsewhere)},
	})
	snap, err := work.BuildSnapshot([]work.Kind{k})
	if err != nil {
		t.Fatal(err)
	}

	var gotOrder []string
	tiers := map[string]int{}
	labels := map[string]string{}
	for _, c := range snap.Containers {
		if c.Kind != ref.KindRoutine {
			t.Fatalf("container %q has kind %q", c.ID, c.Kind)
		}
		gotOrder = append(gotOrder, c.ID)
		tiers[c.ID] = c.RoutineTier
		labels[c.ID] = c.Project
	}
	wantOrder := []string{"zzz-here", "mmm-sibling", "aaa-elsewhere"}
	if !slices.Equal(gotOrder, wantOrder) {
		t.Fatalf("order = %v, want %v", gotOrder, wantOrder)
	}
	wantTiers := map[string]int{"zzz-here": TierHere, "mmm-sibling": TierProject, "aaa-elsewhere": TierElsewhere}
	for id, want := range wantTiers {
		if tiers[id] != want {
			t.Fatalf("%s tier = %d, want %d", id, tiers[id], want)
		}
	}
	// Membership derives from the bound directory alone: the label a container
	// carries is the project that directory belongs to.
	if labels["mmm-sibling"] != "pop" || labels["aaa-elsewhere"] != "other" {
		t.Fatalf("project labels = %v, want sibling under pop and the unrelated one under other", labels)
	}
	// The header counts the page and how much of it is here — "here" being tier 1,
	// so a global list never misreports how much belongs to where you stand.
	if want := "3 routines · 1 here · 3 paused"; snap.SummaryLine() != want {
		t.Fatalf("summary = %q, want %q", snap.SummaryLine(), want)
	}
}

// TestRoutineKindLessReadsOnlyTheStampedTier pins the comparator as pure over two
// containers: a kind wired with no location at all still orders a stamped pair,
// because the cwd was consulted at Load and never here.
func TestRoutineKindLessReadsOnlyTheStampedTier(t *testing.T) {
	k := NewKind(&KindDeps{Routine: &Deps{}})
	elsewhere := work.Container{ID: "aaa", RoutineTier: TierElsewhere}
	hereRow := work.Container{ID: "zzz", RoutineTier: TierHere}
	if !k.Less(hereRow, elsewhere) || k.Less(elsewhere, hereRow) {
		t.Fatal("tier must dominate the id")
	}
	sameTier := []work.Container{{ID: "b", RoutineTier: TierHere}, {ID: "a", RoutineTier: TierHere}}
	sort.SliceStable(sameTier, func(i, j int) bool { return k.Less(sameTier[i], sameTier[j]) })
	if sameTier[0].ID != "a" {
		t.Fatalf("within a tier the order is alphabetical, got %+v", sameTier)
	}
	if k.Less(hereRow, hereRow) {
		t.Fatal("Less(c, c) must be false")
	}
}

// TestRoutineKindLoadsProjectRoutineAsTheSameKind pins the Project routine as one
// more Routine container — a `project:<name>` id and a badge cell, not a fourth
// Work kind — and as something the daemon can never fire.
func TestRoutineKindLoadsProjectRoutineAsTheSameKind(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	mkdirs(t, checkout)
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	writeProjectRoutine(t, checkout, "newrelic", "---\nagents:\n  - claude\n---\nResearch NewRelic bugs.\n")

	k := kindOver(d, canonical(t, checkout), "pop", []project.ExpandedProject{{Name: "pop", ProjectLabel: "pop", Path: canonical(t, checkout)}})
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "project:newrelic")
	if c.Kind != ref.KindRoutine {
		t.Fatalf("kind = %q, want %q", c.Kind, ref.KindRoutine)
	}
	if c.Badge != projectRoutineBadge {
		t.Fatalf("badge = %q, want %q", c.Badge, projectRoutineBadge)
	}
	if c.RoutineTier != TierHere {
		t.Fatalf("tier = %d, want %d — a Project routine is committed to the checkout you are in", c.RoutineTier, TierHere)
	}
	if c.RoutineSchedule != "manual" || c.RoutinePaused {
		t.Fatalf("container = %+v, want a manual schedule and no pause bit", c)
	}
	if want := canonical(t, checkout); c.RoutineDirectory != want || c.Checkout != want {
		t.Fatalf("directory = %q / checkout = %q, want %q", c.RoutineDirectory, c.Checkout, want)
	}
	// No schedule means no consent, so the advance half never offers it.
	candidates, err := k.Candidates()
	if err != nil {
		t.Fatal(err)
	}
	for _, cand := range candidates {
		if cand.Ref.ContainerID == c.ID {
			t.Fatalf("Project routine surfaced as an advance candidate: %+v", cand)
		}
	}
}

// TestRoutineKindLoadsUnreadableRoutineAsBrokenContainer pins the death of the
// warnings side channel: one bad file neither fails the load nor arrives beside
// the page — it is a container carrying BROKEN and the parse error a reader needs.
func TestRoutineKindLoadsUnreadableRoutineAsBrokenContainer(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	mkdirs(t, home)
	d := routineDeps(t, dataHome)
	for _, id := range []string{"alpha", "broken"} {
		if _, err := AddWith(d, id, "daily at 10:00", home); err != nil {
			t.Fatal(err)
		}
	}
	breakManifest(t, dataHome, "broken")

	k := kindOver(d, canonical(t, home), "pop", []project.ExpandedProject{{Name: "pop", ProjectLabel: "pop", Path: canonical(t, home)}})
	containers, err := k.Load()
	if err != nil {
		t.Fatalf("one unreadable Routine must not fail the load: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("containers = %+v, want both routines", containers)
	}
	broken := containerByID(t, containers, "broken")
	if !broken.Broken || broken.Status != BrokenStatus {
		t.Fatalf("broken container = %+v, want Broken with status %q", broken, BrokenStatus)
	}
	if len(broken.DetailSections) != 1 || !strings.Contains(broken.DetailSections[0].Body, "broken") {
		t.Fatalf("detail sections = %+v, want the parse error", broken.DetailSections)
	}
	if broken.BrokenReason != broken.DetailSections[0].Body {
		t.Fatalf("reason %q and section %q disagree", broken.BrokenReason, broken.DetailSections[0].Body)
	}
	// Nothing its definition would have said is invented.
	if broken.RoutineSchedule != "" || broken.RoutineDirectory != "" || broken.Checkout != "" {
		t.Fatalf("broken container invented cells: %+v", broken)
	}
	if healthy := containerByID(t, containers, "alpha"); healthy.Broken {
		t.Fatalf("healthy container marked broken: %+v", healthy)
	}
}

// TestRoutineKindKeepsRoutineWhoseDirectoryVanished pins the never-prune rule: a
// Routine outlives the directory it was bound to, says so in its cell, and offers
// no path for a verb to land in.
func TestRoutineKindKeepsRoutineWhoseDirectoryVanished(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	here := filepath.Join(root, "main")
	gone := filepath.Join(root, "gone")
	mkdirs(t, here, gone)
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "stray", "daily at 10:00", gone); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	k := kindOver(d, canonical(t, here), "pop", []project.ExpandedProject{{Name: "pop", ProjectLabel: "pop", Path: canonical(t, here)}})
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := containerByID(t, containers, "stray")
	if c.RoutineDirectory != MissingDirectoryCell {
		t.Fatalf("directory cell = %q, want %q", c.RoutineDirectory, MissingDirectoryCell)
	}
	if c.RoutineTier != TierElsewhere {
		t.Fatalf("tier = %d, want %d — nobody stands in a directory that is gone", c.RoutineTier, TierElsewhere)
	}
	if c.Checkout != "" {
		t.Fatalf("checkout = %q, want none", c.Checkout)
	}
	if _, err := k.Perform(c, nil, work.VerbShell); err == nil {
		t.Fatal("a shell must refuse rather than land nowhere")
	}
}

// TestRoutineKindLoadWritesNothingAndRegistersNothing pins both halves of the
// membership rule: the read is pure, and a Routine earns no registry row — its
// bound directory already holds the fact a row would restate.
func TestRoutineKindLoadWritesNothingAndRegistersNothing(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	mkdirs(t, home)
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "daily", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}

	k := kindOver(d, canonical(t, home), "pop", []project.ExpandedProject{{Name: "pop", ProjectLabel: "pop", Path: canonical(t, home)}})
	before := dataDirDigest(t, dataHome)
	if _, err := k.Load(); err != nil {
		t.Fatal(err)
	}
	if after := dataDirDigest(t, dataHome); after != before {
		t.Fatal("Load wrote to the data dir; the read path must be pure")
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.AllWorkContainers()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Ref.Kind == ref.KindRoutine {
			t.Fatalf("Routine earned a registry row: %+v", row)
		}
	}
}

// TestRoutineColumnsAreTheTableHeaders keeps the header in one place: the kind
// authors the cells, so the table it renders reads under the kind's own list.
func TestRoutineColumnsAreTheTableHeaders(t *testing.T) {
	k := NewKind(&KindDeps{Routine: &Deps{}})
	if !slices.Equal(k.Columns(), dashboardTableHeaders()) {
		t.Fatalf("Columns() = %v, table headers = %v", k.Columns(), dashboardTableHeaders())
	}
}

// dataDirDigest hashes every file under dir with its path, so any write — a new
// file, a changed byte, a materialised database — changes the digest.
func dataDirDigest(t *testing.T, dir string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum.Write([]byte(path))
		sum.Write(data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}
