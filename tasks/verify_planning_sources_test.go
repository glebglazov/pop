package tasks

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifierPlanningSourcesAndClaims(t *testing.T) {
	t.Parallel()
	tasks := doneAFKSet()
	tasks[0].PlanningSources = []string{"ticket"}
	tasks = append(tasks, Task{ID: "sign-off", File: "sign-off.md", Title: "Human approval", Type: "HITL", Status: TaskOpen, PlanningSources: []string{"ticket"}})
	sources := []PlanningSource{
		{ID: "ticket", Reference: "https://tracker.example/PROJ-42", Title: "Retry policy"},
		{ID: "dropped", Reference: "PROJ-43", Title: "Missing slice"},
	}
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), tasks, map[string]any{"planning_sources": sources})
	var prompt string
	_, _, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		PlanningSourcesConvention: func(cwd string) (string, error) {
			if cwd != "/rt" {
				t.Fatalf("source convention checkout = %q", cwd)
			}
			return "Use the configured tracker reader to fetch each reference.", nil
		},
		runVerifier: func(p string) (string, error) {
			prompt = p
			return "VERDICT: NEEDS-HUMAN\nFINDINGS: PROJ-43 has no claims", nil
		},
	}, m, StatusDone)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"### ticket: Retry policy", "Reference: https://tracker.example/PROJ-42",
		"sign-off [HITL] (open): Human approval", tasks[0].ID + " [AFK] (done)",
		"### dropped: Missing slice", "No task claims this source — report a divergence.",
		"Use the configured tracker reader", "Fetch each declared source", "compare its intent with the acceptance criteria",
		"without presuming which side is wrong", "remediate the implementation, or amend the source and Accept",
		"Any divergence requires VERDICT: NEEDS-HUMAN", "Never use FIXABLE for Intent drift",
		"unrunnable gate: return NEEDS-HUMAN, never PASS or FIXABLE", "published Verify report",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "### sign-off") {
		t.Fatal("HITL task entered the judged work")
	}
}

func TestVerifierWithoutPlanningSourcesKeepsPrompt(t *testing.T) {
	t.Parallel()
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	want := buildVerifierPrompt(d, m, "sha1", workDiffView{}, "", "", "")
	var got string
	_, _, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		PlanningSourcesConvention: func(string) (string, error) { t.Fatal("source-free set resolved the convention"); return "", nil },
		runVerifier:               func(p string) (string, error) { got = p; return "VERDICT: PASS", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || strings.Contains(got, "## Planning sources") {
		t.Fatalf("source-free prompt changed:\n%s", got)
	}
}

func TestVerifierPlanningSourcesConventionFallback(t *testing.T) {
	t.Parallel()
	for _, resolver := range []PlanningSourcesConvention{nil, func(string) (string, error) { return "", errors.New("resolution failed") }} {
		d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
		m.PlanningSources = []PlanningSource{{ID: "ticket", Reference: "PROJ-42"}}
		_, _, err := drainVerifyPhase(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{}, PlanningSourcesConvention: resolver,
			runVerifier: func(p string) (string, error) {
				if !strings.Contains(p, "Read `pop conventions get planning-sources` in full") {
					t.Fatalf("missing convention fallback:\n%s", p)
				}
				return "VERDICT: NEEDS-HUMAN\nFINDINGS: cannot fetch PROJ-42", nil
			},
		}, m, StatusDone)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanningSourceFindingsParkWithoutRemediationAndPublishReport(t *testing.T) {
	for _, findings := range []string{
		"Intent drift: PROJ-42 says retry twice; the criteria say retry three times. Remediate the implementation, or amend the source and Accept.",
		"Intent drift: PROJ-42 has no task claims.",
		"Unrunnable gate: cannot fetch PROJ-42 because credentials are missing.",
	} {
		t.Run(findings, func(t *testing.T) {
			run, refresh, row, indexPath := newVerifyPhaseRunWithKeys(t, func(p string) (string, error) {
				if !strings.Contains(p, "Reference: PROJ-42") || !strings.Contains(p, "Read the tracker with its configured tool.") {
					t.Fatalf("missing source inputs:\n%s", p)
				}
				return "VERDICT: NEEDS-HUMAN\nFINDINGS: " + findings + "\n", nil
			}, map[string]any{"planning_sources": []PlanningSource{{ID: "ticket", Reference: "PROJ-42"}}})
			run.resolved = &ResolvedPaths{DefinitionPath: filepath.Dir(filepath.Dir(indexPath))}
			run.opts.PlanningSourcesConvention = func(string) (string, error) { return "Read the tracker with its configured tool.", nil }
			directive, err := run.verifyPhase(refresh, row)
			if err == nil || directive != verifyReturn || row.Status != StatusVerifyFailed || !run.result.TaskSetVerifyFailed {
				t.Fatalf("set did not park: directive=%v status=%v result=%+v err=%v", directive, row.Status, run.result, err)
			}
			m := LoadManifest(run.d, "demo", indexPath)
			if !m.Valid || remediationDepth(m) != 0 {
				t.Fatalf("unexpected remediation or malformed manifest: %+v", m)
			}
			docs := listVerifyReports(t, m.Dir)
			if len(docs) != 1 {
				t.Fatalf("reports = %v", docs)
			}
			body, err := os.ReadFile(filepath.Join(m.Dir, VerifyDirName, docs[0]))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), findings) {
				t.Fatalf("report lost findings:\n%s", body)
			}
		})
	}
}

func TestVerifierCanPassWhenPlanningSourcesAgree(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	indexPath := filepath.Join(defPath, "demo", "index.json")
	m := LoadManifest(d, "demo", indexPath)
	m.PlanningSourcesExplicit = true
	m.PlanningSources = []PlanningSource{{ID: "ticket", Reference: "PROJ-42"}}
	m.Tasks[0].PlanningSources = []string{"ticket"}
	if err := WriteManifestAtomic(d, m); err != nil {
		t.Fatal(err)
	}
	result, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		PlanningSourcesConvention: func(string) (string, error) { return "Fetch the tracker item with the configured reader.", nil },
		runVerifier: func(p string) (string, error) {
			if !strings.Contains(p, "Fetch the tracker item with the configured reader.") || !strings.Contains(p, "01-a [AFK] (done)") {
				t.Fatalf("standalone verification lost source context:\n%s", p)
			}
			return "VERDICT: PASS\nFINDINGS: Read PROJ-42; its intent agrees with 01-a's criteria and the implementation meets them.", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != VerdictPass {
		t.Fatalf("verdict = %v, want PASS", result.Verdict)
	}
}
