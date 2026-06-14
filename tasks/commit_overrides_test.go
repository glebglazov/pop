package tasks

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

// recordedCommit captures the args passed to a single `git commit` invocation.
type recordedCommit struct {
	args []string
}

// mockGitRecordingCommits returns a MockGit that satisfies the add/diff/commit/
// rev-parse calls both commit helpers make, recording every `commit` invocation.
func mockGitRecordingCommits(commits *[]recordedCommit) *deps.MockGit {
	return &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			// Locate the "commit" verb; with overrides it is preceded by
			// `-c key=value` pairs, so it is not necessarily args[0].
			for _, a := range args {
				if a == "commit" {
					*commits = append(*commits, recordedCommit{args: append([]string(nil), args...)})
					return "", nil
				}
			}
			switch {
			case len(args) >= 1 && args[0] == "diff":
				// Report staged changes so the commit proceeds.
				return "impl.txt\n", nil
			case len(args) >= 1 && args[0] == "rev-parse":
				return "deadbeef\n", nil
			default:
				return "", nil
			}
		},
	}
}

// assertConfigOverrides verifies the recorded commit args begin with the
// expected `-c key=value` pairs (in order) followed by the "commit" verb.
func assertConfigOverrides(t *testing.T, got []string, overrides []string) {
	t.Helper()
	want := make([]string, 0, len(overrides)*2)
	for _, kv := range overrides {
		want = append(want, "-c", kv)
	}
	if len(got) < len(want)+1 {
		t.Fatalf("commit args too short: %v", got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("commit arg[%d] = %q, want %q (full: %v)", i, got[i], w, got)
		}
	}
	if got[len(want)] != "commit" {
		t.Fatalf("expected %q after overrides, got %q (full: %v)", "commit", got[len(want)], got)
	}
}

// assertNoConfigOverrides verifies the recorded commit args carry no `-c` pairs
// — i.e. the invocation is byte-for-byte what it would be today.
func assertNoConfigOverrides(t *testing.T, got []string) {
	t.Helper()
	if len(got) == 0 || got[0] != "commit" {
		t.Fatalf("expected commit verb first with no overrides, got %v", got)
	}
	for _, a := range got {
		if a == "-c" {
			t.Fatalf("unexpected -c override on commit args: %v", got)
		}
	}
}

func TestCreateImplementationCommitAppliesConfigOverrides(t *testing.T) {
	overrides := []string{"commit.gpgsign=false", "user.signingkey="}
	var commits []recordedCommit
	d := &Deps{Git: mockGitRecordingCommits(&commits)}

	if _, err := createImplementationCommit(d, "/runtime", "set", "01-task", "summary", overrides); err != nil {
		t.Fatalf("createImplementationCommit: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected exactly one commit, got %d", len(commits))
	}
	assertConfigOverrides(t, commits[0].args, overrides)
}

func TestCheckpointDirtyRuntimeAppliesConfigOverrides(t *testing.T) {
	overrides := []string{"commit.gpgsign=false"}
	var commits []recordedCommit
	d := &Deps{Git: mockGitRecordingCommits(&commits)}

	if err := checkpointDirtyRuntime(d, "/runtime", "set", "01-task", overrides); err != nil {
		t.Fatalf("checkpointDirtyRuntime: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected exactly one commit, got %d", len(commits))
	}
	assertConfigOverrides(t, commits[0].args, overrides)
}

func TestCommitsCarryNoOverridesWhenUnconfigured(t *testing.T) {
	var commits []recordedCommit
	d := &Deps{Git: mockGitRecordingCommits(&commits)}

	if _, err := createImplementationCommit(d, "/runtime", "set", "01-task", "summary", nil); err != nil {
		t.Fatalf("createImplementationCommit: %v", err)
	}
	if err := checkpointDirtyRuntime(d, "/runtime", "set", "01-task", nil); err != nil {
		t.Fatalf("checkpointDirtyRuntime: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected two commits (implementation + checkpoint), got %d", len(commits))
	}
	for _, c := range commits {
		assertNoConfigOverrides(t, c.args)
	}
}

// TestRunTaskMalformedOverridesFailsDrainHard asserts the drain fails hard with
// a clear, indexed error before any agent or git work runs — resolution happens
// up front, ahead of the dirty-runtime checkpoint that commits earliest.
func TestRunTaskMalformedOverridesFailsDrainHard(t *testing.T) {
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TaskConfig{Git: &config.TaskGitConfig{
			CommitConfigOverrides: []string{"this-is-not-valid"},
		}}}, nil
	}
	d := &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    deps.NewRealGit(),
		Runner: RealCommandRunner{},
	}
	opts := RunTaskOptions{
		ResolveInput: ResolveInput{CWD: t.TempDir()},
		// Non-empty AgentCmd skips agent-output resolution, isolating the
		// override-resolution failure.
		AgentCmd: "true",
		Yes:      true,
	}
	_, err := RunTaskWith(d, nil, loadConfig, opts)
	assertExitCode(t, err, ExitSetup)
	if err == nil || !strings.Contains(err.Error(), "[tasks.git] commit_config_overrides[0]:") {
		t.Fatalf("err = %v, want [tasks.git] commit_config_overrides[0]: error", err)
	}
}

func TestCommitGitArgsEmptyOverridesReturnsArgsUnchanged(t *testing.T) {
	args := commitGitArgs(nil, "commit", "-m", "subject")
	if strings.Join(args, " ") != "commit -m subject" {
		t.Fatalf("expected unchanged args, got %v", args)
	}
}
