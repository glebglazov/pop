package routine

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/frontmatter"
	"github.com/glebglazov/pop/project"
)

// projectEditDeps wires interactive routine Deps rooted in `checkout`: a
// capturing editor, scripted stdin, captured stdout, and Project deps reporting
// `checkout` as the current worktree.
func projectEditDeps(t *testing.T, dataHome, checkout, input string, out io.Writer) *Deps {
	t.Helper()
	d := checkoutDeps(t, dataHome, checkout)
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{Routines: &config.RoutinesConfig{Agents: []string{"claude"}}}, nil
	}
	d.IsInteractive = func() bool { return true }
	d.Stdin = strings.NewReader(input)
	d.Stdout = out
	d.OpenEditor = func(string) error { return nil }
	d.OpenPager = func(string) error { return nil }
	return d
}

func projectRoutinePath(checkout, name string) string {
	return filepath.Join(checkout, ".pop", "routines", name+".md")
}

func TestEditProjectPromptOpensInRepoFile(t *testing.T) {
	for _, ref := range []string{"audit", "project:audit"} {
		t.Run(ref, func(t *testing.T) {
			root := t.TempDir()
			dataHome := filepath.Join(root, "data")
			checkout := filepath.Join(root, "checkout")
			d := projectEditDeps(t, dataHome, checkout, "", io.Discard)
			writeProjectRoutine(t, checkout, "audit", "---\nagents:\n  - claude\n---\nAudit the config.\n")

			var opened string
			d.OpenEditor = func(path string) error { opened = path; return nil }

			res, err := EditWith(d, ref, "", false)
			if err != nil {
				t.Fatalf("EditWith(%q): %v", ref, err)
			}
			want := projectRoutinePath(checkout, "audit")
			if opened != want {
				t.Fatalf("opened %q, want %q", opened, want)
			}
			if res.RoutineID != "project:audit" || res.PromptPath != want || !res.Opened {
				t.Fatalf("result = %+v, want project:audit opened at %q", res, want)
			}
			if res.ScheduleUpdated {
				t.Fatal("editing a Project routine prompt must not touch its schedule")
			}
			// A Project routine has no pause state: no state.json is written anywhere
			// under the per-checkout data root.
			key := checkoutKey(checkout)
			stateFilePath := filepath.Join(projectRoutineDataDir(d, key, "audit"), stateFileName)
			if _, err := os.Stat(stateFilePath); err == nil {
				t.Fatalf("edit wrote pause state at %s; a Project routine has none", stateFilePath)
			}
		})
	}
}

func TestEditProjectPromptNonInteractiveNamesInRepoPath(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectEditDeps(t, dataHome, checkout, "", io.Discard)
	d.IsInteractive = func() bool { return false }
	editorCalled := false
	d.OpenEditor = func(string) error { editorCalled = true; return nil }
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")

	_, err := EditWith(d, "project:audit", "", false)
	if err == nil {
		t.Fatal("expected error on non-interactive Project prompt edit")
	}
	if !strings.Contains(err.Error(), projectRoutinePath(checkout, "audit")) {
		t.Fatalf("error should name the in-repo path, got %v", err)
	}
	if editorCalled {
		t.Fatal("editor must not open in a non-interactive session")
	}
}

func TestProjectRuntimeEditRewritesInRepoFrontmatter(t *testing.T) {
	for _, ref := range []string{"audit", "project:audit"} {
		t.Run(ref, func(t *testing.T) {
			root := t.TempDir()
			dataHome := filepath.Join(root, "data")
			checkout := filepath.Join(root, "checkout")
			d := projectEditDeps(t, dataHome, checkout, "", io.Discard)
			writeProjectRoutine(t, checkout, "audit", "---\nagents:\n  - claude\n---\nAudit the config.\n")

			res, err := UpdateRuntimeWith(d, ref, []string{"codex"}, true, "heavy", true)
			if err != nil {
				t.Fatalf("UpdateRuntimeWith(%q): %v", ref, err)
			}
			// A Project routine has no pause state, so a runtime edit never pauses.
			if res.Paused {
				t.Fatal("editing a Project routine's runtime config must not pause it")
			}
			if res.RoutineID != "project:audit" {
				t.Fatalf("RoutineID = %q, want project:audit", res.RoutineID)
			}
			data, err := os.ReadFile(projectRoutinePath(checkout, "audit"))
			if err != nil {
				t.Fatal(err)
			}
			fields, body, err := frontmatter.Parse(string(data))
			if err != nil {
				t.Fatal(err)
			}
			if len(fields.Agents) != 1 || fields.Agents[0] != "codex" || fields.Effort != "heavy" {
				t.Fatalf("frontmatter = %+v, want agents [codex] effort heavy", fields)
			}
			// The prompt body is preserved verbatim.
			if body != "Audit the config.\n" {
				t.Fatalf("body = %q, want the untouched prompt", body)
			}
			// No state.json is written for a Project routine.
			key := checkoutKey(checkout)
			stateFilePath := filepath.Join(projectRoutineDataDir(d, key, "audit"), stateFileName)
			if _, err := os.Stat(stateFilePath); err == nil {
				t.Fatalf("runtime edit wrote pause state at %s; a Project routine has none", stateFilePath)
			}
		})
	}
}

// recordingProjectDeps builds project.Deps that report `checkout` as the current
// worktree while recording every git invocation, so a test can prove pop never
// stages or commits a Project routine edit.
func recordingProjectDeps(checkout string, calls *[][]string) *project.Deps {
	return &project.Deps{
		FS: &deps.MockFileSystem{
			GetwdFunc: func() (string, error) { return checkout, nil },
		},
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				*calls = append(*calls, append([]string(nil), args...))
				if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel" {
					return checkout, nil
				}
				return "", fmt.Errorf("unexpected git call: %v", args)
			},
		},
	}
}

func TestProjectEditNeverStagesOrCommits(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectEditDeps(t, dataHome, checkout, "", io.Discard)
	var gitCalls [][]string
	d.Project = recordingProjectDeps(checkout, &gitCalls)
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")

	if _, err := EditWith(d, "project:audit", "", false); err != nil {
		t.Fatalf("edit prompt: %v", err)
	}
	if _, err := UpdateRuntimeWith(d, "project:audit", []string{"codex"}, true, "", false); err != nil {
		t.Fatalf("runtime edit: %v", err)
	}
	for _, c := range gitCalls {
		if len(c) > 0 && (c[0] == "add" || c[0] == "commit") {
			t.Fatalf("a Project routine edit invoked git %q; pop must never stage or commit", c[0])
		}
	}
}

func TestRefineProjectRoutineRendersProjectAwareMenu(t *testing.T) {
	for _, ref := range []string{"audit", "project:audit"} {
		t.Run(ref, func(t *testing.T) {
			root := t.TempDir()
			dataHome := filepath.Join(root, "data")
			checkout := filepath.Join(root, "checkout")
			var out bytes.Buffer
			d := projectEditDeps(t, dataHome, checkout, "0\n", &out)
			writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")

			if err := RefineWith(d, ref, ""); err != nil {
				t.Fatalf("RefineWith(%q): %v", ref, err)
			}
			text := out.String()
			for _, want := range []string{
				`Refine Project routine "project:audit"`,
				"manual-fire-only",
				"1. Agent session (default)",
				"2. Fire test run",
				"3. View last report",
				"4. Edit prompt",
				"0. Exit",
				`Leaving Project routine "project:audit".`,
			} {
				if !strings.Contains(text, want) {
					t.Fatalf("menu missing %q:\n%s", want, text)
				}
			}
			// A Project routine has no schedule and no pause state, so the schedule
			// and resume gate items never appear.
			for _, absent := range []string{"Edit schedule", "Resume routine"} {
				if strings.Contains(text, absent) {
					t.Fatalf("Project refine menu must not offer %q:\n%s", absent, text)
				}
			}
		})
	}
}

func TestRefineProjectRoutineNonInteractiveNamesInRepoPath(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectEditDeps(t, dataHome, checkout, "", io.Discard)
	d.IsInteractive = func() bool { return false }
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")

	err := RefineWith(d, "project:audit", "")
	if err == nil {
		t.Fatal("expected refusal in a non-interactive session")
	}
	if !strings.Contains(err.Error(), projectRoutinePath(checkout, "audit")) {
		t.Fatalf("error should name the in-repo path, got %v", err)
	}
}

func TestBuildProjectAuthoringPromptIsProjectAware(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectEditDeps(t, dataHome, checkout, "", io.Discard)
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit the config for drift.\n")

	pr, err := findProjectRoutine(d, "audit")
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildProjectAuthoringPrompt(d, pr)
	for _, want := range []string{
		"Project routine",
		"manual-fire-only",
		"NO schedule and none may be set",
		"agents` and `effort` only",
		"pop NEVER commits",
		projectRoutinePath(checkout, "audit"),
		"Audit the config for drift.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("briefing missing %q:\n%s", want, prompt)
		}
	}
}

func TestRefinePaneWithProjectRoutineSpawnsProjectEdit(t *testing.T) {
	d, _ := routineTmuxDeps(t)
	checkout := t.TempDir()
	var gitCalls [][]string
	d.Project = recordingProjectDeps(checkout, &gitCalls)
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")

	if _, err := RefinePaneWith(d, "project:audit", ""); err != nil {
		t.Fatalf("RefinePaneWith: %v", err)
	}
	rt := tmuxRecorder(d)
	newWindow, ok := rt.findCommand("new-window")
	if !ok {
		t.Fatal("expected new-window")
	}
	// The `:` in project:audit is folded to `_` so tmux does not misread the
	// session:window target.
	if !containsArg(newWindow, "-n", "project_audit") {
		t.Fatalf("new-window = %v, want -n project_audit", newWindow)
	}
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected send-keys")
	}
	joined := strings.Join(sendKeys, " ")
	if !strings.Contains(joined, "/mock/bin/pop routine edit project:audit") {
		t.Fatalf("send-keys = %v, want project:audit edit command", sendKeys)
	}
}

func TestPromptPathForEditTargetsInRepoFileForProject(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectEditDeps(t, dataHome, checkout, "", io.Discard)
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")

	got := promptPathForEdit(d, "project:audit")
	want := projectRoutinePath(checkout, "audit")
	if got != want {
		t.Fatalf("promptPathForEdit = %q, want in-repo file %q", got, want)
	}
}
