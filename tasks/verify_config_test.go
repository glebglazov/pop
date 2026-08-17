package tasks

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

// manifestWithVerifier builds a bare manifest carrying a per-set `verifier`
// override object under Unknown, so VerifierOverride() parses it exactly as it
// would from a loaded index.json.
func manifestWithVerifier(t *testing.T, agents []string, effort string) *Manifest {
	t.Helper()
	over := VerifierDirective{Agents: agents, Effort: effort}
	raw, err := json.Marshal(over)
	if err != nil {
		t.Fatal(err)
	}
	return &Manifest{Unknown: map[string]json.RawMessage{"verifier": raw}}
}

// TestResolveVerifierPrecedence covers the Verifier precedence chain (ADR-0086),
// highest first: CLI flags → per-set manifest override → [work.verify] →
// [work.implement].agents / heavy, with agents and effort resolving independently.
func TestResolveVerifierPrecedence(t *testing.T) {
	verifyCfg := func(agents []string, effort string) *config.Config {
		return &config.Config{Work: &config.WorkConfig{
			Verify: &config.VerifyConfig{Enabled: true, Agents: config.AgentEntriesFromCommands(agents...), Effort: effort},
		}}
	}
	defaultAgentsCfg := func(defaults []string, v *config.VerifyConfig) *config.Config {
		return &config.Config{Work: &config.WorkConfig{Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands(defaults...)}, Verify: v}}
	}

	tests := []struct {
		name       string
		cliAgents  []string
		cliEffort  string
		manifest   *Manifest
		cfg        *config.Config
		wantAgents []string
		wantEffort string
	}{
		{
			name:       "default when nothing configured",
			wantAgents: []string{DefaultAgentPreset},
			wantEffort: DefaultVerifyEffort,
		},
		{
			name:       "config drives agents and effort",
			cfg:        verifyCfg([]string{"codex", "claude"}, "standard"),
			wantAgents: []string{"codex", "claude"},
			wantEffort: "standard",
		},
		{
			name:       "omitted config agents fall back to default_agents",
			cfg:        defaultAgentsCfg([]string{"cursor"}, &config.VerifyConfig{Enabled: true}),
			wantAgents: []string{"cursor"},
			wantEffort: DefaultVerifyEffort,
		},
		{
			name:       "per-set overrides config",
			manifest:   manifestWithVerifier(t, []string{"pi"}, "light"),
			cfg:        verifyCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"pi"},
			wantEffort: "light",
		},
		{
			name:       "CLI overrides per-set and config",
			cliAgents:  []string{"opencode"},
			cliEffort:  "heavy",
			manifest:   manifestWithVerifier(t, []string{"pi"}, "light"),
			cfg:        verifyCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"opencode"},
			wantEffort: "heavy",
		},
		{
			name:       "agents and effort resolve independently",
			cliAgents:  []string{"opencode"},
			manifest:   manifestWithVerifier(t, nil, "light"),
			cfg:        verifyCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"opencode"}, // CLI agents win
			wantEffort: "light",              // per-set effort wins (no CLI effort)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := resolveVerifier(tt.cliAgents, tt.cliEffort, tt.manifest, tt.cfg)
			if err != nil {
				t.Fatalf("resolveVerifier: %v", err)
			}
			if strings.Join(sel.Agents, ",") != strings.Join(tt.wantAgents, ",") {
				t.Fatalf("agents = %#v, want %#v", sel.Agents, tt.wantAgents)
			}
			if sel.Effort != tt.wantEffort {
				t.Fatalf("effort = %q, want %q", sel.Effort, tt.wantEffort)
			}
		})
	}
}

// TestVerifierBinaryAvailable confirms the PATH check the fall-through relies on:
// a resolvable binary is available, an unresolvable one is not, and an unknown
// preset is never available.
func TestVerifierBinaryAvailable(t *testing.T) {
	d := &Deps{LookPath: func(file string) (string, error) {
		if file == "claude" {
			return "/usr/bin/claude", nil
		}
		return "", exec.ErrNotFound
	}}
	if !agentBinaryAvailable(d, "claude") {
		t.Fatal("claude should be available (LookPath resolves it)")
	}
	if agentBinaryAvailable(d, "cursor") {
		t.Fatal("cursor should be unavailable (LookPath does not resolve cursor-agent)")
	}
	if agentBinaryAvailable(d, "not-a-preset") {
		t.Fatal("an unknown preset must never be available")
	}
}

// TestRunConfiguredVerifierAllMissingHardErrors: when every configured agent's
// binary is absent, the runner hard-errors with each preset's diagnostic instead
// of yielding empty output that ParseVerdict would turn into NEEDS-HUMAN.
func TestRunConfiguredVerifierAllMissingHardErrors(t *testing.T) {
	d := newTestDeps(t)
	d.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	_, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"cursor", "claude"}, Effort: "heavy",
	}, t.TempDir(), "demo", "sha1", t.TempDir(), "prompt", &bytes.Buffer{}, &bytes.Buffer{}, time.Minute, nil)
	assertExitCode(t, err, ExitSetup)
	errText := err.Error()
	if !strings.Contains(errText, "all configured agents unavailable") {
		t.Fatalf("error = %q, want exhaustion message", errText)
	}
	if !strings.Contains(errText, "cursor: \"binary not found on PATH\"") {
		t.Fatalf("error = %q, want cursor missing-binary diagnostic", errText)
	}
	if !strings.Contains(errText, "claude: \"binary not found on PATH\"") {
		t.Fatalf("error = %q, want claude missing-binary diagnostic", errText)
	}
}

// TestRunConfiguredVerifierFallsThroughMissingBinary: the runner skips the first
// agent (binary missing on PATH) and runs the next available one, returning its
// verdict text. It uses a real fake `claude` binary on a controlled PATH so the
// missing-binary fall-through is exercised end-to-end through the real spawn.
func TestRunConfiguredVerifierFallsThroughMissingBinary(t *testing.T) {
	// ADR-0145: PATH stub — stays serial deliberately.
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"result\",\"result\":\"VERDICT: PASS\"}'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Only binDir is on PATH: cursor-agent is missing, the fake claude is found.
	t.Setenv("PATH", binDir)

	taskSetDir := t.TempDir()
	// Real-subprocess smoke: verify fall-through spawns the fake claude on PATH
	// (see realShimSmokeSet).
	d := &Deps{FS: testFSWithDataHome(t.TempDir()), Runner: RealCommandRunner{}, LookPath: exec.LookPath}
	out, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"cursor", "claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", t.TempDir(), "prompt", &bytes.Buffer{}, &bytes.Buffer{}, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(out, "VERDICT: PASS") {
		t.Fatalf("output = %q, want the fallback claude's PASS verdict", out)
	}
	if v, _, _ := ParseVerdict(out); v != VerdictPass {
		t.Fatalf("verdict = %q, want PASS from the fallback agent", v)
	}

	// The fallback claude invocation is persisted as a verify Captured run.
	pairs := listRunFilePairs(t, capturedRunsDir(taskSetDir))
	if len(pairs) != 1 {
		t.Fatalf("want 1 persisted verify run, got %d", len(pairs))
	}
	meta := readCapturedRunMeta(t, pairs[0].meta)
	if meta.Phase != "verify" || meta.TaskSetID != "demo" || meta.WorkSHA != "sha1" {
		t.Fatalf("persisted meta = %+v", meta)
	}
	if meta.Verdict != "PASS" {
		t.Fatalf("verdict = %q, want PASS", meta.Verdict)
	}
}
