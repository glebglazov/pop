package tasks

import (
	"strings"
	"testing"
)

// argvOf resolves one preset's headless command line as a single string, which
// is what these tests judge: the posture is a property of the argument vector
// pop hands execve, not of any struct on the way there.
func argvOf(t *testing.T, preset string, readOnly bool) string {
	t.Helper()
	var (
		invocation *AgentInvocation
		err        error
	)
	if readOnly {
		invocation, err = ResolveReadOnlyAgentInvocation(preset, "", "PROMPT", "/tmp/checkout", AgentOutputAuto)
	} else {
		invocation, err = ResolveAgentInvocationWithMode(preset, "", "PROMPT", "/tmp/checkout", AgentOutputAuto)
	}
	if err != nil {
		t.Fatalf("%s: resolve: %v", preset, err)
	}
	return strings.Join(append([]string{invocation.Name}, invocation.Args...), " ")
}

func TestReadOnlyPostureReachesTheRefinerCommandLine(t *testing.T) {
	supported := map[string][]string{
		"claude": {"--disallowedTools=Edit,Write,NotebookEdit"},
		"codex":  {"--sandbox", "read-only"},
		"cursor": {"--mode", "ask"},
		"pi":     {"--exclude-tools", "edit,write"},
	}
	for preset, args := range supported {
		t.Run(preset, func(t *testing.T) {
			argv := argvOf(t, preset, true)
			if want := strings.Join(args, " "); !strings.Contains(argv, want) {
				t.Fatalf("read-only argv should carry %q, got %q", want, argv)
			}
			// The prompt is the last thing on the line, never an argument a
			// variadic posture flag could swallow.
			if !strings.HasSuffix(argv, " PROMPT") {
				t.Fatalf("read-only argv should end with the prompt, got %q", argv)
			}
			if plain := argvOf(t, preset, false); strings.Contains(plain, strings.Join(args, " ")) {
				t.Fatalf("posture leaked into an ordinary invocation: %q", plain)
			}
		})
	}
}

// codex is the one preset whose headless prefix contradicts the posture: with
// --dangerously-bypass-approvals-and-sandbox on the line, codex reports
// `sandbox: danger-full-access` and the sandbox pop asked for never takes.
func TestCodexReadOnlyPostureWithdrawsTheSandboxBypass(t *testing.T) {
	argv := argvOf(t, "codex", true)
	if strings.Contains(argv, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("read-only codex should not bypass its own sandbox: %q", argv)
	}
	if !strings.Contains(argv, "--skip-git-repo-check") {
		t.Fatalf("withdrawal should take back only the bypass flag: %q", argv)
	}
	if plain := argvOf(t, "codex", false); !strings.Contains(plain, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("an ordinary codex run keeps its bypass: %q", plain)
	}
}

func TestBlindPresetStillRunsAndSaysThePostureIsNotEnforced(t *testing.T) {
	for _, preset := range []string{"opencode", "kimi"} {
		t.Run(preset, func(t *testing.T) {
			readOnly := argvOf(t, preset, true)
			if plain := argvOf(t, preset, false); readOnly != plain {
				t.Fatalf("a blind preset should launch unchanged:\n read-only: %q\n plain:     %q", readOnly, plain)
			}
			invocation, err := ResolveReadOnlyAgentInvocation(preset, "", "PROMPT", "/tmp/checkout", AgentOutputAuto)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			note, enforced := invocation.ReadOnlyPosture()
			if enforced {
				t.Fatalf("%s cannot enforce a posture it declared blind", preset)
			}
			reason := mustAdapter(t, preset).ReadOnlyPostureCapability().Reason
			if !strings.Contains(note, "not enforced") || !strings.Contains(note, reason) {
				t.Fatalf("note should say the posture was not obtained and why, got %q", note)
			}
		})
	}
}

func TestSupportedPresetReportsThePostureItObtained(t *testing.T) {
	invocation, err := ResolveReadOnlyAgentInvocation("claude", "", "PROMPT", "/tmp/checkout", AgentOutputAuto)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	note, enforced := invocation.ReadOnlyPosture()
	if !enforced || !strings.Contains(note, "--disallowedTools=Edit,Write,NotebookEdit") {
		t.Fatalf("claude should report the arguments enforcing its posture, got %q (enforced=%v)", note, enforced)
	}
	plain, err := ResolveAgentInvocationWithMode("claude", "", "PROMPT", "/tmp/checkout", AgentOutputAuto)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if note, _ := plain.ReadOnlyPosture(); note != "" {
		t.Fatalf("an invocation nobody asked to run read-only reports no posture, got %q", note)
	}
}

// The posture is declared on every preset and spawned by no role: the Refiner
// was its only consumer, and a Refiner that fixes in place cannot run without
// its editing tools (ADR-0240). The capability stays for the next read-only
// role, which is what the tests above cover.
func TestNoRoleSpawnsUnderTheReadOnlyPosture(t *testing.T) {
	for name, role := range map[string]agentRole{
		"Refiner":  refinerRole(nil, nil, "", "", ""),
		"Verifier": verifierRole(nil, nil, "", "", ""),
	} {
		if role.ReadOnly {
			t.Fatalf("the %s is spawned with no read-only arguments", name)
		}
	}
}

// The licence replaces the prohibition: the frame states what the pass may
// change instead of forbidding every change, because an agent told what it may
// do fixes better than one that discovers a tool is missing. ADR-0248 widens
// safe-and-local to reversible.
func TestRefinerPromptStatesItsFixLicence(t *testing.T) {
	prompt := buildRefinerPrompt(bareDeps(), goldenBareManifest(),
		workDiffView{Range: "root000..HEAD", Stat: " a.go | 1 +"}, "", "", passDocument{}, false)
	if strings.Contains(prompt, "Change no files") {
		t.Fatal("the Refiner prompt no longer forbids changing files")
	}
	if strings.Contains(prompt, "safe and local") {
		t.Fatal("the Refiner prompt no longer frames the licence as safe-and-local")
	}
	if strings.Contains(prompt, "anything structural") {
		t.Fatal("the Refiner prompt no longer treats anything structural as report-only")
	}
	for _, want := range []string{
		"where the fix is reversible",
		"can this be undone by inspection, and can I see its whole effect?",
		"Fix nothing the standard does not name.",
		"pop conventions get verification",
		"Do not commit",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the Refiner prompt must state its licence (%q missing):\n%s", want, prompt)
		}
	}
}

func mustAdapter(t *testing.T, preset string) AgentAdapter {
	t.Helper()
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		t.Fatalf("resolve adapter %s: %v", preset, err)
	}
	return adapter
}
