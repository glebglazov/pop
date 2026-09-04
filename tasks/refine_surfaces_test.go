package tasks

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

// refineProse is the one string no surface may print: the document's body. Every
// surface carries a pointer to it and nothing else (ADR-0252).
const refineProse = "SECRET-REFINE-PROSE"

// seedRefineReport files one Refine report for the fixture set, as the
// Refiner would have written it, and returns its path.
func seedRefineReport(t *testing.T, d *Deps, m *Manifest) string {
	t.Helper()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	body := refineReport.renderDocument(at, "demo", "abc123abc123", "aaa111^..HEAD", "claude", "## Naming\n\n"+refineProse)
	path, err := refineReport.writeDocument(d, m.Dir, at, body)
	if err != nil {
		t.Fatalf("writeRefineDocument: %v", err)
	}
	return path
}

func hitlFixture(t *testing.T) (*Deps, *Manifest) {
	t.Helper()
	return setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Sign off", Type: "HITL", Status: "open"},
	}, nil)
}

func hitlGateOutput(t *testing.T, d *Deps, m *Manifest, input string) (string, hitlGateAction) {
	t.Helper()
	return hitlGateOutputWithConfig(t, d, m, nil, input)
}

// hitlGateOutputWithConfig drives the gate under a named configuration — the
// Refine mark is resolved through one, so a test about it cannot use the
// unconfigured default.
func hitlGateOutputWithConfig(t *testing.T, d *Deps, m *Manifest, cfg *config.Config, input string) (string, hitlGateAction) {
	t.Helper()
	var out strings.Builder
	in := strings.NewReader(input)
	verify, hasVerify := latestVerifyPointer(d, m)
	action, err := promptHITLGateAction(&out, in, d, cfg, "/rt", newPromptReader(in), "demo", m, &m.Tasks[1],
		"## Acceptance criteria\n\n- [ ] ok\n", nil, false, resolveGateRefineState(d, cfg, m), verify, hasVerify)
	if err != nil {
		t.Fatalf("promptHITLGateAction: %v", err)
	}
	return out.String(), action
}

// TestRefinePointerReachesEverySurface drives every surface the original refine
// pointer reached. The gate and Assist prompt retain the full pointer, while the
// CLI detail view now gives the Artifact summary and list command (ADR-0217).
func TestRefinePointerReachesEverySurface(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	path := seedRefineReport(t, d, m)

	gate, _ := hitlGateOutput(t, d, m, "0\n")

	var detail bytes.Buffer
	RenderTaskSetDetail(d, nil, &detail, "demo", nil, m)

	assist := BuildAssistPrompt(d, nil, "demo", m, StatusAwaitingApproval, "/rt", "")

	for _, s := range []struct {
		surface string
		text    string
		wants   []string
	}{
		{"HITL gate", gate, []string{path, "abc123a", "aaa111^..HEAD", "Read the refine report"}},
		{"detail view", detail.String(), []string{"ARTIFACTS", "1 artifact", "newest: refine, written 2026-08-16 12:00Z", "pop tasks artifacts demo"}},
		{"assist prompt", assist, []string{path, "abc123a", "read the file yourself"}},
	} {
		for _, want := range s.wants {
			if !strings.Contains(s.text, want) {
				t.Fatalf("%s missing %q:\n%s", s.surface, want, s.text)
			}
		}
		if strings.Contains(s.text, refineProse) {
			t.Fatalf("%s inlined the refine report:\n%s", s.surface, s.text)
		}
	}
	if strings.Contains(detail.String(), path) || strings.Contains(detail.String(), "abc123a") {
		t.Fatalf("detail view retained the refine pointer:\n%s", detail.String())
	}
}

// TestSetWithNoArtifactsShowsNothingExtra pins the empty-list rule on all three
// surfaces. The fixture has task documents, but no published artifacts.
func TestSetWithNoArtifactsShowsNothingExtra(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)

	gate, _ := hitlGateOutput(t, d, m, "0\n")

	var detail bytes.Buffer
	RenderTaskSetDetail(d, nil, &detail, "demo", nil, m)

	assist := BuildAssistPrompt(d, nil, "demo", m, StatusAwaitingApproval, "/rt", "")

	for _, s := range []struct {
		surface string
		text    string
	}{
		{"HITL gate", gate},
		{"detail view", detail.String()},
		{"assist prompt", assist},
	} {
		lower := strings.ToLower(s.text)
		mentionsEmptyBlock := strings.Contains(lower, "refine report")
		if s.surface == "detail view" {
			mentionsEmptyBlock = strings.Contains(lower, "artifacts")
		}
		if mentionsEmptyBlock {
			t.Fatalf("%s mentions artifacts the set does not have:\n%s", s.surface, s.text)
		}
	}
}

// TestHITLGateReadRefineEntryPagesAndSpawnsNoAgent pins the gate's refine entry:
// it is the next free key, it resolves to the read action, and taking it runs
// the human's pager over the document — no agent, and no change to the set.
func TestHITLGateReadRefineEntryPagesAndSpawnsNoAgent(t *testing.T) {
	d, m := hitlFixture(t)
	path := seedRefineReport(t, d, m)

	// Re-verify is not offered here, so the refine entry takes key 5.
	_, action := hitlGateOutput(t, d, m, "5\n")
	if action != hitlGateReadRefine {
		t.Fatalf("expected the refine entry at key 5, got action %d", action)
	}

	runner := &fakeAttendedRunner{}
	d.Runner = runner
	t.Setenv("PAGER", "mypager -X")
	refine, ok := latestRefinePointer(d, m)
	if !ok {
		t.Fatal("expected a refine pointer")
	}
	var out bytes.Buffer
	pageReportDocument(d, strings.NewReader(""), "/rt", &out, refine)

	if runner.attendedCalls != 1 {
		t.Fatalf("expected exactly one attended launch (the pager), got %d", runner.attendedCalls)
	}
	if runner.name != "mypager" || strings.Join(runner.args, " ") != "-X "+path {
		t.Fatalf("expected the pager over the document, got %s %v", runner.name, runner.args)
	}
}

// TestRefinePointerReadsTheReportsOwnHeader pins where the commit facts come
// from: the header the report carries, not a side-car pop keeps beside it.
func TestRefinePointerReadsTheReportsOwnHeader(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	seedRefineReport(t, d, m)

	p, ok := latestRefinePointer(d, m)
	if !ok {
		t.Fatal("expected a refine pointer")
	}
	if p.WorkSHA != ShortSHA("abc123abc123") || p.CommitRange != "aaa111^..HEAD" {
		t.Fatalf("pointer read the wrong header: %+v", p)
	}
	if got := p.CommitPhrase(); got != ShortSHA("abc123abc123")+" (aaa111^..HEAD)" {
		t.Fatalf("commit phrase: %q", got)
	}
}

// attendedPrompts renders every attended prompt pop opens for a set: the Assist
// session and the four gates. Keyed by name so a failure says which surface lost
// the block.
func attendedPrompts(d *Deps, m *Manifest) map[string]string {
	return map[string]string{
		"assist prompt":         BuildAssistPrompt(d, nil, "demo", m, StatusAwaitingApproval, "/rt", ""),
		"HITL gate prompt":      BuildHITLAssistancePrompt(d, "demo", m, m.Tasks[1], "/rt"),
		"failed gate prompt":    BuildFailedAssistancePrompt(d, "demo", m, m.Tasks[0], "/rt"),
		"verify-failed prompt":  BuildVerifyFailedAssistancePrompt(d, "demo", m, "sha1", "01-a: naming", "/rt"),
		"interrupt gate prompt": BuildInterruptAssistancePrompt(d, "demo", m, m.Tasks[0], "/rt"),
	}
}

// TestRefineBlockReachesEveryAttendedPrompt drives the Refine pointer decision
// end to end: every attended session is told where the report is and whether it
// still describes the checkout, while the unattended implementer and the Verifier
// are told nothing about it.
func TestRefineBlockReachesEveryAttendedPrompt(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	path := seedRefineReport(t, d, m)

	// The fixture checkout's HEAD is sha1; the report was written against
	// abc123abc123, so every prompt must say the report is out of date.
	for surface, text := range attendedPrompts(d, m) {
		for _, want := range []string{path, "abc123abc123", "Out of date", "read the file yourself"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q:\n%s", surface, want, text)
			}
		}
		if strings.Contains(text, refineProse) {
			t.Fatalf("%s inlined the refine report:\n%s", surface, text)
		}
	}

	// The two unattended prompts carry no pointer at all.
	unattended := map[string]string{
		"implementer prompt": BuildAgentPrompt(filepath.Join(m.Dir, m.Tasks[0].File), "/rt", ""),
		"verifier prompt":    buildVerifierPrompt(d, m, "sha1", workDiffView{Range: "aaa111^..HEAD", Stat: " a.go | 1 +"}, "", ""),
	}
	for surface, text := range unattended {
		for _, unwanted := range []string{path, "Latest Refine report"} {
			if strings.Contains(text, unwanted) {
				t.Fatalf("%s carries the refine block (%q):\n%s", surface, unwanted, text)
			}
		}
	}
}

// TestRefineBlockIsSilentWhenCurrentOrAbsent pins the block's two quiet states:
// a report written against the commit the checkout is on says nothing about
// staleness, and a set with no report renders no block in any attended prompt.
func TestRefineBlockIsSilentWhenCurrentOrAbsent(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	seedRefineReport(t, d, m)
	d.Git = stubGit("abc123abc123\n", "", "")

	for surface, text := range attendedPrompts(d, m) {
		if !strings.Contains(text, "Latest Refine report") {
			t.Fatalf("%s lost the refine block:\n%s", surface, text)
		}
		if strings.Contains(text, "Out of date") {
			t.Fatalf("%s called a current report out of date:\n%s", surface, text)
		}
	}

	unrefined, um := hitlFixture(t)
	for surface, text := range attendedPrompts(unrefined, um) {
		if strings.Contains(strings.ToLower(text), "refine report") {
			t.Fatalf("%s mentions a report the set does not have:\n%s", surface, text)
		}
	}
}

// TestRefineStalenessNeedsBothCommits pins that an unknown commit on either side
// is not staleness — a document with no work SHA, or a checkout pop cannot read,
// says nothing about whether the report still holds.
func TestRefineStalenessNeedsBothCommits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		pointer RefinePointer
		current string
		stale   bool
	}{
		{name: "moved on", pointer: RefinePointer{WorkSHA: "abc123abc123"}, current: "def456def456", stale: true},
		{name: "same commit", pointer: RefinePointer{WorkSHA: "abc123abc123"}, current: "abc123abc123", stale: false},
		{name: "same commit unabbreviated", pointer: RefinePointer{WorkSHA: "abc123abc123"}, current: "abc123abc123def789", stale: false},
		{name: "shorter header", pointer: RefinePointer{WorkSHA: "abc123a"}, current: "abc123abc123", stale: false},
		{name: "document records none", pointer: RefinePointer{}, current: "abc123abc123", stale: false},
		{name: "checkout unreadable", pointer: RefinePointer{WorkSHA: "abc123abc123"}, current: "", stale: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.pointer.StaleAgainst(tc.current); got != tc.stale {
				t.Fatalf("StaleAgainst(%q) = %v, want %v", tc.current, got, tc.stale)
			}
		})
	}
}

// TestAssistPromptPointsAtTheImplementationConvention pins the Assist hint (ADR-0252):
// the session is told where the standard is written, and told that running the
// pass itself is not its business — the Refiner commits, and an attended session
// leaves committing to the human.
func TestAssistPromptPointsAtTheImplementationConvention(t *testing.T) {
	t.Parallel()
	m := &Manifest{Stem: "demo", Dir: t.TempDir(), Valid: true,
		Tasks: []Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: TaskOpen}}}
	prompt := BuildAssistPrompt(&Deps{FS: &promptFixtureFS{files: map[string]string{}}}, nil, "demo", m, StatusReady, "/rt", "")

	for _, want := range []string{
		"`pop conventions get implementation`",
		"Do not invoke `pop tasks implement`, `pop tasks verify` or `pop tasks refine`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("assist prompt is missing %q:\n%s", want, prompt)
		}
	}
}

// TestRefineMarkReachesEverySurface drives the mark onto the three surfaces a
// reader can walk into a set from without passing the sign-off gate: the detail
// view, the gate's paging entry, and the Assist prompt (ADR-0260 decision 5).
// Each set here has a report on disk, which is the situation the mark exists
// for: an interrupted pass publishes nothing and leaves the previous report in
// place, so a report alone cannot say whether this changeset was refined.
func TestRefineMarkReachesEverySurface(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		stream string
		answer string
		want   string
	}{
		{name: "refined", stream: streamOutcomeCompleted, answer: refineReply("refined"), want: "Refined"},
		{name: "a pass that did not finish", stream: streamOutcomeInterrupted, want: "Not refined"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d, m := hitlFixture(t)
			seedRefineReport(t, d, m)
			recordRefinePass(t, d, m.Dir, at, tc.stream, tc.answer)
			cfg := refineEnabledConfig()

			var detail bytes.Buffer
			RenderTaskSetDetail(d, cfg, &detail, "demo", nil, m)
			assist := BuildAssistPrompt(d, cfg, "demo", m, StatusAwaitingApproval, "/rt", "")
			gate, _ := hitlGateOutputWithConfig(t, d, m, cfg, "0\n")
			// Everything below the entry's label is the entry's own detail, which
			// keeps this assertion off the preamble printed above the menu.
			_, entry, found := strings.Cut(gate, "Read the refine report")
			if !found {
				t.Fatalf("gate offered no paging entry:\n%s", gate)
			}

			for surface, text := range map[string]string{
				"detail view":   detail.String(),
				"paging entry":  entry,
				"assist prompt": assist,
			} {
				if !strings.Contains(text, tc.want) {
					t.Fatalf("%s does not say %q:\n%s", surface, tc.want, text)
				}
				if tc.want == "Refined" && strings.Contains(text, "Not refined") {
					t.Fatalf("%s calls a refined set unrefined:\n%s", surface, text)
				}
			}
		})
	}
}

// TestRefineMarkIsSilentOnTheSurfacesWhenAbsent: a set Refine does not apply to
// carries no mark, and no surface invents one — the same silence a set with no
// report gets.
func TestRefineMarkIsSilentOnTheSurfacesWhenAbsent(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	seedRefineReport(t, d, m)

	var detail bytes.Buffer
	RenderTaskSetDetail(d, nil, &detail, "demo", nil, m)
	assist := BuildAssistPrompt(d, nil, "demo", m, StatusAwaitingApproval, "/rt", "")

	for surface, text := range map[string]string{"detail view": detail.String(), "assist prompt": assist} {
		if strings.Contains(text, "Not refined") || strings.Contains(text, "\U0001F4DD") {
			t.Fatalf("%s marked a set Refine does not apply to:\n%s", surface, text)
		}
	}
}
