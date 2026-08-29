package tasks

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
)

// houseConvention is the prose a `/to-tasks` run would have recorded on the set,
// and renderedSubject is what a Verifier reading it renders for a fix — no
// `tasks(...)` prefix anywhere, so a commit made under it is unmistakably the
// convention's and not pop's default format.
const (
	houseConvention = "Conventional Commits: <type>(<scope>): <summary>, imperative mood, no trailing period."
	renderedSubject = "fix(auth): stop dropping the refresh token on retry"
)

// TestVerifyPhaseSpawnsRemediationWithRenderedSubject drives the real verify
// phase for a FIXABLE verdict and follows the Planned commit subject the Verifier
// rendered onto the spawned Remediation task (ADR-0207). The three cases are the
// whole rule: a set carrying a convention gets the rendered subject, a set
// without one is never asked and takes nothing even when the response volunteers
// a subject, and an unusable rendering degrades to no subject while the
// remediation itself still spawns.
func TestVerifyPhaseSpawnsRemediationWithRenderedSubject(t *testing.T) {
	for _, tc := range []struct {
		name       string
		convention string
		response   string
		want       string
	}{
		{
			name:       "rendered under the set's convention",
			convention: houseConvention,
			response:   "VERDICT: FIXABLE\nSUMMARY: token dropped on retry\nCOMMIT-SUBJECT: " + renderedSubject + "\nFINDINGS: criterion 2 unmet\n",
			want:       renderedSubject,
		},
		{
			name:     "no convention on the set",
			response: "VERDICT: FIXABLE\nCOMMIT-SUBJECT: " + renderedSubject + "\nFINDINGS: criterion 2 unmet\n",
			want:     "",
		},
		{
			name:       "unrendered placeholder degrades to the default format",
			convention: houseConvention,
			response:   "VERDICT: FIXABLE\nCOMMIT-SUBJECT: <type>(<scope>): <summary>\nFINDINGS: criterion 2 unmet\n",
			want:       "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setKeys := map[string]any{}
			if tc.convention != "" {
				setKeys["commit_convention"] = tc.convention
			}
			prompt := ""
			run, refresh, row, indexPath := newVerifyPhaseRunWithKeys(t, func(p string) (string, error) {
				prompt = p
				return tc.response, nil
			}, setKeys)

			directive, err := run.verifyPhase(refresh, row)
			if err != nil {
				t.Fatalf("verifyPhase: %v", err)
			}
			if directive != verifyContinue {
				t.Fatalf("directive = %d, want verifyContinue (%d) — the remediation must spawn either way", directive, verifyContinue)
			}
			m := LoadManifest(run.d, "demo", indexPath)
			if !m.Valid {
				t.Fatalf("reloaded manifest invalid: %v", m.Errors)
			}
			spawned := taskByID(t, m, "02-remediation")
			if spawned.CommitSubject != tc.want {
				t.Fatalf("spawned commit_subject = %q, want %q", spawned.CommitSubject, tc.want)
			}
			// The Verifier is only asked for a subject when the set has a convention to
			// render one under.
			asked := strings.Contains(prompt, "COMMIT-SUBJECT:")
			if asked != (tc.convention != "") {
				t.Fatalf("prompt asks for a commit subject = %v, want %v", asked, tc.convention != "")
			}
			if tc.convention != "" && !strings.Contains(prompt, tc.convention) {
				t.Fatalf("prompt does not carry the set's convention:\n%s", prompt)
			}
			// However the subject came out, the findings the fixing agent reads are
			// untouched by it.
			body, err := run.d.FS.ReadFile(strings.TrimSuffix(indexPath, "index.json") + spawned.File)
			if err != nil {
				t.Fatalf("read spawned body: %v", err)
			}
			if !strings.Contains(string(body), "criterion 2 unmet") {
				t.Fatalf("spawned body lost the findings:\n%s", body)
			}
		})
	}
}

// TestRemediationSubjectReachesTheCommitVerbatim closes the loop the phase test
// opens: a Remediation task born with a rendered Planned commit subject is
// drained through the real executor and lands in git under that subject, with the
// agent's summary still the body. Nothing in the executor knows the task was
// spawned mid-drain — it takes the planned subject the same way it takes a
// plan-time one.
func TestRemediationSubjectReachesTheCommitVerbatim(t *testing.T) {
	env := setupExecutorFixtureIsolated(t)
	writeManifestWithSetKeys(t, env.demoDir(), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	}, map[string]any{"commit_convention": houseConvention})

	m := loadDemoManifest(t, env)
	subject := remediationCommitSubject(m, &store.VerifyVerdict{
		CommitSubject: parseVerifierCommitSubject("VERDICT: FIXABLE\nCOMMIT-SUBJECT: `" + renderedSubject + "`\nFINDINGS: criterion 2 unmet\n"),
	})
	id, err := spawnRemediationTask(env.deps(), m, "", remediationSpawn{
		WorkSHA:       "deadbeefcafe",
		Findings:      "criterion 2 unmet",
		CommitSubject: subject,
		Origin:        RemediationOriginAuto,
	})
	if err != nil {
		t.Fatalf("spawnRemediationTask: %v", err)
	}

	result := drainTaskWithCommit(t, env, id, "fix.txt")
	got, err := realGitInDir(env.root, "log", "-1", "--format=%s", result.CommitSHA)
	if err != nil {
		t.Fatalf("log %s: %v", result.CommitSHA, err)
	}
	if got != renderedSubject {
		t.Fatalf("remediation commit subject = %q, want %q", got, renderedSubject)
	}
	body, err := realGitInDir(env.root, "log", "-1", "--format=%b", result.CommitSHA)
	if err != nil {
		t.Fatalf("log body: %v", err)
	}
	if first, _, _ := strings.Cut(strings.TrimSpace(body), "\n\n"); first != "summary for "+id {
		t.Fatalf("commit body = %q, want the agent summary", body)
	}
	// A Remediation task is a task, so its commit is marked like any other's.
	if got, want := readTaskTrailer(t, env.root, result.CommitSHA), "demo/"+id; got != want {
		t.Fatalf("Pop-Task trailer = %q, want %q", got, want)
	}
	if recorded := taskByID(t, loadDemoManifest(t, env), id); recorded.Commit == nil || recorded.Commit.Subject != renderedSubject {
		t.Fatalf("recorded commit = %+v, want the rendered subject", recorded.Commit)
	}
}

// TestParseVerifierCommitSubject pins what pop will and will not accept as a
// rendered subject. Everything rejected here falls back to pop's default format
// rather than failing the spawn, so the list is about what is committable, not
// about what is well-formed agent output.
func TestParseVerifierCommitSubject(t *testing.T) {
	t.Parallel()
	long := "fix(auth): " + strings.Repeat("x", agentCommitSubjectMaxLen)
	for _, tc := range []struct{ name, raw, want string }{
		{"plain label", "VERDICT: FIXABLE\nCOMMIT-SUBJECT: " + renderedSubject + "\nFINDINGS: x", renderedSubject},
		{"spaced and bolded label", "VERDICT: FIXABLE\n**COMMIT SUBJECT:** " + renderedSubject, renderedSubject},
		{"underscored label", "VERDICT: FIXABLE\nCOMMIT_SUBJECT: " + renderedSubject, renderedSubject},
		{"backtick wrapped", "VERDICT: FIXABLE\nCOMMIT-SUBJECT: `" + renderedSubject + "`", renderedSubject},
		{"absent", "VERDICT: FIXABLE\nFINDINGS: criterion 2 unmet", ""},
		{"empty value", "VERDICT: FIXABLE\nCOMMIT-SUBJECT:\nFINDINGS: x", ""},
		{"unrendered placeholder", "VERDICT: FIXABLE\nCOMMIT-SUBJECT: <type>(<scope>): <summary>", ""},
		{"prose too long to be a subject", "VERDICT: FIXABLE\nCOMMIT-SUBJECT: " + long, ""},
		{"inside the findings body", "VERDICT: FIXABLE\nFINDINGS: the fix\nCOMMIT-SUBJECT: " + renderedSubject, ""},
		{"no colon", "VERDICT: FIXABLE\nCOMMIT SUBJECTS are conventional here", ""},
	} {
		if got := parseVerifierCommitSubject(tc.raw); got != tc.want {
			t.Fatalf("%s: subject = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestCommitSubjectLineIsNotFindings: a response with no FINDINGS label hands the
// lines after the verdict to the fixing agent as its findings. The rendered
// subject is an instruction to pop, not a finding, so it must not land in that
// body — the same exemption the SUMMARY line has.
func TestCommitSubjectLineIsNotFindings(t *testing.T) {
	t.Parallel()
	_, findings, _ := ParseVerdict("VERDICT: FIXABLE\nCOMMIT-SUBJECT: " + renderedSubject + "\nthe widget never renders\n")
	if findings != "the widget never renders" {
		t.Fatalf("findings = %q, want the commit subject line excluded", findings)
	}
}
