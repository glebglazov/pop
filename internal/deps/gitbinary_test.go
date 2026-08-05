package deps

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// resetGitBinary drops the process-wide resolution for one test. Production
// code wants the memo to outlive any PATH change; a test that swaps git for a
// fake needs the next fork to see it.
func resetGitBinary(t *testing.T) {
	t.Helper()

	previous := processGitBinary
	processGitBinary = &gitBinaryMemo{resolve: previous.resolve}
	t.Cleanup(func() { processGitBinary = previous })
}

func TestGitBinaryResolvesOncePerProcess(t *testing.T) {
	resolutions := 0
	memo := &gitBinaryMemo{resolve: func() string {
		resolutions++
		return "/resolved/git"
	}}

	for i := 0; i < 3; i++ {
		if got := memo.get(); got != "/resolved/git" {
			t.Fatalf("call %d: got %q", i, got)
		}
	}
	if resolutions != 1 {
		t.Fatalf("resolved %d times, want 1", resolutions)
	}
}

func TestResolveGitBinaryPrefersEnvironmentGitAndForksNothing(t *testing.T) {
	// A real git on PATH that records being run: resolution must return it
	// without ever executing it, since a probing fork would spend the ~20ms it
	// is trying to save.
	dir := t.TempDir()
	marker := filepath.Join(dir, "forked")
	envGit := filepath.Join(dir, "git")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(envGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got := resolveGitBinary(os.Getenv, exec.LookPath, isExecutableFile)
	if got != envGit {
		t.Fatalf("got %q, want the environment's git %q", got, envGit)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("resolution forked git: marker stat err = %v", err)
	}
}

func TestResolveGitBinaryOverrideBeatsWorkingEnvironmentGit(t *testing.T) {
	env := func(key string) string {
		if key == GitBinaryEnvVar {
			return "/somewhere/else/git"
		}
		return ""
	}
	lookPath := func(string) (string, error) { return "/opt/homebrew/bin/git", nil }

	got := resolveGitBinary(env, lookPath, func(string) bool { return true })
	if got != "/somewhere/else/git" {
		t.Fatalf("got %q, want the override", got)
	}
}

func TestResolveGitBinaryConsultsCandidatesOnlyForAppleStub(t *testing.T) {
	stubPath := func(string) (string, error) { return appleGitStub, nil }
	present := func(paths ...string) func(string) bool {
		return func(path string) bool {
			for _, p := range paths {
				if p == path {
					return true
				}
			}
			return false
		}
	}

	tests := []struct {
		name       string
		lookPath   func(string) (string, error)
		executable func(string) bool
		want       string
	}{
		{
			name:       "homebrew wins over the xcode developer path",
			lookPath:   stubPath,
			executable: present("/opt/homebrew/bin/git", "/Applications/Xcode.app/Contents/Developer/usr/bin/git"),
			want:       "/opt/homebrew/bin/git",
		},
		{
			name:       "usr local before xcode",
			lookPath:   stubPath,
			executable: present("/usr/local/bin/git", "/Applications/Xcode.app/Contents/Developer/usr/bin/git"),
			want:       "/usr/local/bin/git",
		},
		{
			name:       "no candidate present falls back to the stub",
			lookPath:   stubPath,
			executable: present(),
			want:       appleGitStub,
		},
		{
			name:       "a non-stub environment git is never traded for a candidate",
			lookPath:   func(string) (string, error) { return "/nix/store/bin/git", nil },
			executable: present("/opt/homebrew/bin/git"),
			want:       "/nix/store/bin/git",
		},
		{
			name:       "no git on PATH still names git",
			lookPath:   func(string) (string, error) { return "", errors.New("not found") },
			executable: present("/opt/homebrew/bin/git"),
			want:       "git",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveGitBinary(func(string) string { return "" }, tc.lookPath, tc.executable)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitBinaryRunsRealGit(t *testing.T) {
	out, err := NewRealGit().Command("--version")
	if err != nil {
		t.Fatalf("git --version through the resolved binary: %v", err)
	}
	if out == "" {
		t.Fatal("git --version returned nothing")
	}
}
