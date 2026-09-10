package binding

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestFoldLandingExitAndReentryChoices(t *testing.T) {
	for _, choice := range []string{"land", "abandon", "yes", "noninteractive"} {
		t.Run(choice, func(t *testing.T) {
			t.Parallel()
			repo := initAdoptRepo(t)
			td := lifecycleTestDeps(t)
			wt := addLinkedWorktree(t, repo, "human-work")
			writeFileCommit(t, wt, "clash.txt", "branch\n", "branch clash")
			writeFileCommit(t, repo, "clash.txt", "trunk\n", "trunk clash")
			before, trunk := refAt(t, wt, "human-work"), refAt(t, repo, "HEAD")
			scratch := foldScratchBranch("human-work")
			cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
			if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: strings.NewReader("0\n")}, io.Discard); err == nil {
				t.Fatal("want parked conflict")
			}
			if err := os.WriteFile(filepath.Join(wt, "clash.txt"), []byte("resolved\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGitOutput(t, wt, "add", "clash.txt")
			var out strings.Builder
			input := "2\n0\n"
			if choice == "land" {
				td.Runner = &foldConflictResolverRunner{setPath: wt}
				input = "\n0\n"
			}
			if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: strings.NewReader(input)}, &out); err == nil || !strings.Contains(err.Error(), "is parked") {
				t.Fatalf("exit: %v\n%s", err, &out)
			}
			folded := refAt(t, wt, scratch)
			if refAt(t, repo, "HEAD") != trunk || refAt(t, wt, "human-work") != before || rebaseInProgress(td, wt) {
				t.Fatal("exit must preserve the completed rebase without moving either real branch")
			}
			for _, want := range []string{"Fold landing gate:", "1. Land now (default)", "3. Abandon fold", "0. Exit", "Commits over trunk: 1"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q:\n%s", want, &out)
				}
			}
			for _, ref := range []string{CurrentBranch(td, repo), "human-work", scratch} {
				evidence := strings.TrimSpace(runGitOutput(t, wt, "log", "-1", "--format=%h %s", ref))
				if !regexp.MustCompile(regexp.QuoteMeta(ref+" — "+evidence) + ` \([^)]* ago\)`).MatchString(out.String()) {
					t.Errorf("missing tip evidence %s", evidence)
				}
			}
			out.Reset()
			opts := FoldOptions{In: strings.NewReader("\n")}
			switch choice {
			case "abandon":
				opts.In = strings.NewReader("3\n")
			case "yes":
				opts.Yes = true
				opts.In = strings.NewReader("0\n")
			case "noninteractive":
				opts.In = tasks.NonInteractiveReader{}
			}
			_, err := FoldCheckout(td, cfg, wt, opts, &out)
			if choice == "abandon" {
				if !errors.Is(err, tasks.ErrFoldAbandon) {
					t.Fatalf("abandon: %v", err)
				}
				if refAt(t, repo, "HEAD") != trunk || refAt(t, wt, "human-work") != before {
					t.Fatal("abandon moved a real branch")
				}
			} else {
				if err != nil {
					t.Fatalf("land: %v\n%s", err, &out)
				}
				if refAt(t, repo, "HEAD") != folded || refAt(t, wt, "human-work") != folded {
					t.Fatal("fold did not land the rebased tip")
				}
			}
			if branchExists(t, repo, scratch) || CurrentBranch(td, wt) != "human-work" {
				t.Fatal("scratch cleanup did not restore the checkout")
			}
			wantGate := choice == "land" || choice == "abandon"
			if strings.Contains(out.String(), "Fold landing gate:") != wantGate {
				t.Fatalf("unexpected gate output:\n%s", &out)
			}
		})
	}
}

func TestFoldLandingVerifyReturnsWithBadge(t *testing.T) {
	for _, verdict := range []string{"PASS", "FIXABLE"} {
		t.Run(verdict, func(t *testing.T) {
			t.Parallel()
			repo := initAdoptRepo(t)
			td := lifecycleTestDeps(t)
			seedDoneTaskSet(t, td, repo, "set-verify-landing")
			wt := addLinkedWorktree(t, repo, "human-work")
			writeFileCommit(t, wt, "feature.txt", "work\n", "branch work")
			writeFileCommit(t, repo, "trunk.txt", "trunk\n", "trunk work")
			scratch := foldScratchBranch("human-work")
			trunkBranch := CurrentBranch(td, repo)
			runGitOutput(t, wt, "checkout", "-b", scratch)
			runGitOutput(t, wt, "rebase", trunkBranch)
			trunk, folded := refAt(t, repo, "HEAD"), refAt(t, wt, "HEAD")
			var out strings.Builder
			calls := 0
			err := tasks.HandleFoldLanding(td, &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}, tasks.FoldConflictContext{
				SetID: "set-verify-landing", RuntimePath: wt, TrunkPath: repo,
				SetBranch: "human-work", TrunkBranch: trunkBranch, ScratchBranch: scratch,
			}, tasks.FoldConflictAssistanceOptions{In: strings.NewReader("2\n0\n"), Out: &out,
				RunVerifier: func(prompt string) (string, error) {
					calls++
					return "VERDICT: " + verdict + "\nFINDINGS: checked\n", nil
				},
			})
			if err == nil || !strings.Contains(err.Error(), "is parked") {
				t.Fatalf("verify then exit: %v\n%s", err, &out)
			}
			if calls != 1 || strings.Count(out.String(), "Fold landing gate:") != 2 {
				t.Fatalf("verification did not return to gate: calls=%d\n%s", calls, &out)
			}
			badge := "verified @ " + folded[:12]
			if verdict != "PASS" {
				badge = "unverified"
			}
			if !strings.Contains(out.String(), badge) {
				t.Fatalf("missing updated badge:\n%s", &out)
			}
			if refAt(t, repo, "HEAD") != trunk || refAt(t, wt, scratch) != folded {
				t.Fatal("verification moved a ref")
			}
		})
	}
}

func TestCleanFoldHasNoLandingGate(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, repo, "human-work")
	writeFileCommit(t, wt, "feature.txt", "work\n", "work")
	var out strings.Builder
	_, err := FoldCheckout(td, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, wt, FoldOptions{In: strings.NewReader("0\n")}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Fold landing gate:") {
		t.Fatalf("clean fold opened gate:\n%s", &out)
	}
	if refAt(t, repo, "HEAD") != refAt(t, wt, "human-work") {
		t.Fatal("clean fold did not land")
	}
}
