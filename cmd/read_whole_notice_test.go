package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/glebglazov/pop/conventions"
)

// noticedBlock is one kind's stretch of a command's output: the notice line and
// everything the reader was told to read.
type noticedBlock struct {
	claimed int
	printed int
	body    string
}

// splitNotices cuts a command's output into the blocks a reader sees, one per
// kind, and counts each block for itself. It counts rather than matching a
// golden because the fact worth pinning is that the notice does not lie — a
// golden would instead break on every editorial pass over a shipped answer
// (ADR-0250).
func splitNotices(t *testing.T, out string) []noticedBlock {
	t.Helper()
	separator := "\n" + strings.Repeat("=", 72) + "\n\n"
	var blocks []noticedBlock
	for _, section := range strings.Split(out, separator) {
		head, rest, ok := strings.Cut(section, "\n")
		if !ok {
			t.Fatalf("a printed section has no notice line above it:\n%s", section)
		}
		if !strings.HasPrefix(head, conventions.ReadWholeNoticeLabel) {
			t.Fatalf("a printed section does not open with the read-whole notice:\n%s", section)
		}
		var claimed int
		if _, err := fmt.Sscanf(head, conventions.ReadWholeNoticeLabel+": %d lines follow.", &claimed); err != nil {
			t.Fatalf("the notice does not state a line count: %q", head)
		}
		printed := len(strings.Split(strings.TrimSuffix(rest, "\n"), "\n"))
		blocks = append(blocks, noticedBlock{claimed: claimed, printed: printed, body: rest})
	}
	return blocks
}

// TestConventionsGetPrintsANoticeThatCountsItsOwnBlock is ADR-0250's guard on
// the resolve verb: every kind is preceded by one notice, and the number in it
// is the number of lines the reader actually received.
func TestConventionsGetPrintsANoticeThatCountsItsOwnBlock(t *testing.T) {
	f := newConventionFixture(t)
	// An overlay is written for one kind so the counted block includes the
	// appended layer and the provenance line, not just the answer.
	f.overlay(t, "commits", "Always name the task in a trailer.\n")

	for _, kind := range conventions.Kinds() {
		var out bytes.Buffer
		if err := runConventionsGetWith(f.deps.conventionsDeps(), &out, f.repo, []string{string(kind)}); err != nil {
			t.Fatalf("conventions get %s: %v", kind, err)
		}
		blocks := splitNotices(t, out.String())
		if len(blocks) != 1 {
			t.Fatalf("get %s printed %d notices, want 1", kind, len(blocks))
		}
		if blocks[0].claimed != blocks[0].printed {
			t.Errorf("get %s: notice claims %d lines, printed %d", kind, blocks[0].claimed, blocks[0].printed)
		}
		if !strings.Contains(blocks[0].body, "CONVENTION "+string(kind)) {
			t.Errorf("get %s: the notice sits above the kind's banner, not below it:\n%s", kind, blocks[0].body)
		}
	}

	// With no kind named, each kind is counted for itself: one total for the
	// whole run would match no document a reader could check it against.
	var all bytes.Buffer
	if err := runConventionsGetWith(f.deps.conventionsDeps(), &all, f.repo, nil); err != nil {
		t.Fatalf("conventions get: %v", err)
	}
	blocks := splitNotices(t, all.String())
	if len(blocks) != len(conventions.Kinds()) {
		t.Fatalf("get printed %d notices for %d kinds", len(blocks), len(conventions.Kinds()))
	}
	for i, b := range blocks {
		if b.claimed != b.printed {
			t.Errorf("kind %d of the full run: notice claims %d lines, printed %d", i, b.claimed, b.printed)
		}
	}
}

// TestConventionsDefaultPrintsTheSameNotice: pop's own answer is read through a
// command too, so it is guarded on the same terms.
func TestConventionsDefaultPrintsTheSameNotice(t *testing.T) {
	for _, kind := range conventions.Kinds() {
		var out bytes.Buffer
		if err := runConventionsDefaultWith(&out, string(kind)); err != nil {
			t.Fatalf("conventions default %s: %v", kind, err)
		}
		blocks := splitNotices(t, out.String())
		if len(blocks) != 1 {
			t.Fatalf("default %s printed %d notices, want 1", kind, len(blocks))
		}
		if blocks[0].claimed != blocks[0].printed {
			t.Errorf("default %s: notice claims %d lines, printed %d", kind, blocks[0].claimed, blocks[0].printed)
		}
		if !strings.Contains(blocks[0].body, "SHIPPED CONVENTION "+string(kind)) {
			t.Errorf("default %s: the notice sits above the banner:\n%s", kind, blocks[0].body)
		}
	}
}

// TestNoticeStaysOnTheCommandPaths: on the two paths pop hands the prose over
// itself the notice would tell an agent not to shorten output it never ran, so
// it must be absent from the prompt bodies, from the manifest key pop projects,
// and from the Config dashboard's scrolled preview (ADR-0250).
func TestNoticeStaysOnTheCommandPaths(t *testing.T) {
	f := newConventionFixture(t)
	td := f.deps.tasksDeps()
	for name, seam := range map[string]func(string) (string, error){
		"the Verifier's prompt body":       verificationConvention(td),
		"the Refiner's implementation convention": implementationConvention(td),
		"the manifest's commit_convention": commitConvention(td),
	} {
		prose, err := seam(f.repo)
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		if strings.Contains(prose, conventions.ReadWholeNoticeLabel) {
			t.Errorf("%s carries the read-whole notice:\n%s", name, prose)
		}
	}

	for _, kind := range conventions.Kinds() {
		stack, err := conventions.Resolve(f.deps.conventionsDeps(), kind, f.repo)
		if err != nil {
			t.Fatalf("resolve %s: %v", kind, err)
		}
		if preview := conventions.StackPreview(stack); strings.Contains(preview, conventions.ReadWholeNoticeLabel) {
			t.Errorf("the Config dashboard's %s preview carries the read-whole notice:\n%s", kind, preview)
		}
	}
}

// TestRegisteredSetsCommitConventionCarriesNoNotice walks the whole register
// path rather than the seam: the key on disk is what a Verifier reads, and a
// notice inside it would be an instruction to re-run a command nothing ran.
func TestRegisteredSetsCommitConventionCarriesNoNotice(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	setDir := registerConventionSet(t, tasksDir, "2026-08-20-notice", "")
	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("tasks register: %v", err)
	}
	var recorded string
	if err := json.Unmarshal(readSetKeys(t, setDir)["commit_convention"], &recorded); err != nil {
		t.Fatalf("commit_convention is not a string: %v", err)
	}
	if strings.TrimSpace(recorded) == "" {
		t.Fatal("register recorded no commit convention, so the test proves nothing")
	}
	if strings.Contains(recorded, conventions.ReadWholeNoticeLabel) {
		t.Errorf("the registered commit convention carries the read-whole notice:\n%s", recorded)
	}
}
