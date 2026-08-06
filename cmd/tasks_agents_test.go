package cmd

// Batch/agent cmd tests stay serial: they stub package-level
// taskStdinInteractive / runTaskMultiSelect / taskConfigLoad hooks (ADR-0145).


import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestTaskAgentsCatalogListsPresetsWithEffortLadders(t *testing.T) {
	found := map[string]bool{
		"claude":   true,
		"opencode": true,
		"codex":    true,
	}
	var looked []string
	d := &tasks.Deps{
		FS: cmdTestFS(filepath.Join(t.TempDir(), "xdg"), ""),
		LookPath: func(file string) (string, error) {
			looked = append(looked, file)
			if found[file] {
				return "/mock/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
	}
	oldLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Effort: map[string]config.EffortConfig{
			"opencode": {
				Heavy:    []config.EffortModel{{Model: "opencode/claude-opus-4-8", Reasoning: "high"}, {Model: "opencode/kimi-k2.6"}},
				Standard: []config.EffortModel{{Model: "opencode/claude-sonnet-4-6", Reasoning: "medium"}},
				Light:    []config.EffortModel{{Model: "opencode/kimi-k2.6"}},
			},
		}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = oldLoad })

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, taskAgentsOptions{}); err != nil {
		t.Fatal(err)
	}

	// Columns five and six are the declaration columns: only claude can be told
	// to cap turns, and only claude, cursor and codex have attended auto-approval
	// arguments (ADR-0190, ADR-0187).
	rows := [][7]string{
		{"agent", "binary", "found", "assist", "turn cap", "attended", "effort ladder"},
		{"claude", "claude", "yes", "yes", "yes", "yes", "heavy: opus[reasoning=high] (built-in); standard: sonnet[reasoning=high] (built-in); light: haiku[reasoning=high] (built-in)"},
		{"opencode", "opencode", "yes", "yes", "no", "no", "heavy: opencode/claude-opus-4-8[reasoning=high], opencode/kimi-k2.6 (configured); standard: opencode/claude-sonnet-4-6[reasoning=medium] (configured); light: opencode/kimi-k2.6 (configured)"},
		{"cursor", "cursor-agent", "no", "yes", "no", "yes", "heavy: composer-2.5 (built-in); standard: composer-2.5 (built-in); light: composer-2.5-fast (built-in)"},
		{"codex", "codex", "yes", "yes", "no", "yes", "heavy: gpt-5.5[reasoning=high] (built-in); standard: gpt-5.5[reasoning=medium] (built-in); light: gpt-5.4-mini[reasoning=low] (built-in)"},
		{"pi", "pi", "no", "yes", "no", "no", "heavy: opencode-go/qwen3.7-max[reasoning=high] (built-in); standard: opencode-go/kimi-k2.6[reasoning=medium] (built-in); light: opencode-go/deepseek-v4-flash[reasoning=low] (built-in)"},
		// kimi is last and opt-in only; its ladder's reasoning reaches the process
		// through the environment, but the catalog renders it like any other.
		{"kimi", "kimi", "no", "yes", "no", "no", "heavy: moonshot-ai/kimi-k3[reasoning=high] (built-in); standard: moonshot-ai/kimi-k3[reasoning=low] (built-in); light: moonshot-ai/kimi-k2.7-code-highspeed (built-in)"},
	}
	var want strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&want, "%-9s %-14s %-5s %-6s %-8s %-8s %s\n", r[0], r[1], r[2], r[3], r[4], r[5], r[6])
	}
	// The table ends by pointing at where every "no" above is explained.
	fmt.Fprint(&want, "\nrun with --why for what a turn cap or attended session actually gets per agent\n")
	if buf.String() != want.String() {
		t.Fatalf("catalog output mismatch\nwant:\n%sgot:\n%s", want.String(), buf.String())
	}

	wantLookups := []string{"claude", "opencode", "cursor-agent", "codex", "pi", "kimi"}
	if strings.Join(looked, ",") != strings.Join(wantLookups, ",") {
		t.Fatalf("lookups = %v, want %v", looked, wantLookups)
	}
}

func TestTaskAgentsModelsListsCuratedAliases(t *testing.T) {
	d := &tasks.Deps{
		FS:       cmdTestFS(filepath.Join(t.TempDir(), "xdg"), ""),
		LookPath: func(file string) (string, error) { return "/mock/bin/" + file, nil },
	}
	oldLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) { return nil, nil }
	t.Cleanup(func() { taskConfigLoad = oldLoad })

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, taskAgentsOptions{Models: true}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"claude    opus, sonnet, haiku, fable\n",
		"kimi      moonshot-ai/kimi-k3, moonshot-ai/kimi-k2.7-code, moonshot-ai/kimi-k2.7-code-highspeed (install-dependent aliases)\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("models section missing\nwant contains:\n%sgot:\n%s", want, got)
		}
	}
	// Curated aliases stay out of the default table. Ladder models legitimately
	// appear there, so this pins alias-only content: the install-dependent marker
	// and a curated entry no ladder tier names.
	defaultTable := strings.SplitN(got, "\n\n", 2)[0]
	for _, unwanted := range []string{"install-dependent aliases", "fable"} {
		if strings.Contains(defaultTable, unwanted) {
			t.Fatalf("models belong in the requested section, not the default table:\n%s", got)
		}
	}
}

func TestTaskAgentsCatalogListsConfigOnlyEffortAgents(t *testing.T) {
	d := &tasks.Deps{
		FS: cmdTestFS(filepath.Join(t.TempDir(), "xdg"), ""),
		LookPath: func(file string) (string, error) {
			if file == "custom-agent" {
				return "/mock/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
	}
	oldLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Effort: map[string]config.EffortConfig{
			"custom-agent": {
				Heavy: []config.EffortModel{{Model: "custom-large", Reasoning: "high"}},
			},
		}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = oldLoad })

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, taskAgentsOptions{}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// A config-only effort agent runs as a custom agent command, so its
	// declaration columns are the custom adapter's: no cap flag pop can know, and
	// no attended form at all.
	want := "custom-agent custom-agent   yes   no     no       no       heavy: custom-large[reasoning=high] (configured); standard: none (configured); light: none (configured)\n"
	if !strings.Contains(got, want) {
		t.Fatalf("config-only agent row missing\nwant contains:\n%sgot:\n%s", want, got)
	}
	// Its rows carry the same reachable reasons as a preset's.
	var why bytes.Buffer
	if err := runTaskAgentsWith(d, &why, taskAgentsOptions{Why: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"custom-agent turn cap       no: a custom agent command is an opaque shell command with no cap flag pop can know\n",
		"custom-agent attended args  no: a custom agent command is a headless command with no attended form\n",
	} {
		if !strings.Contains(why.String(), want) {
			t.Fatalf("--why missing custom-agent line\nwant contains:\n%sgot:\n%s", want, why.String())
		}
	}
}

// TestTaskAgentsWhyReachesDeclaredReasons pins the answer to "I set a turn cap
// and nothing happened" (ADR-0190) and to "what does an attended session
// actually launch with" (ADR-0187): a supported preset shows the argv it emits,
// and an unsupported one shows the adapter's own sentence, off the default
// render but pointed at from it.
func TestTaskAgentsWhyReachesDeclaredReasons(t *testing.T) {
	d := &tasks.Deps{
		FS:       cmdTestFS(filepath.Join(t.TempDir(), "xdg"), ""),
		LookPath: func(file string) (string, error) { return "/mock/bin/" + file, nil },
	}
	oldLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) { return nil, nil }
	t.Cleanup(func() { taskConfigLoad = oldLoad })

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, taskAgentsOptions{Why: true}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"\nagent     capability     declared support\n",
		// The one preset that can be told to cap turns names the flag it emits,
		// with the bound left as N because no run's cap is in hand here.
		"claude    turn cap       yes: --max-turns N\n",
		"claude    attended args  yes: --permission-mode auto\n",
		// pi supports neither, and both of its reasons are reachable here.
		"pi        turn cap       no: pi has no turn cap, and no token or dollar budget either\n",
		"pi        attended args  no: pi's CLI carries no auto-approval flag — not even its headless drains pass one\n",
		"opencode  turn cap       no: opencode's turn cap is its per-agent `steps` key, reachable only from its own config file, which pop never writes\n",
		"kimi      attended args  no: kimi's -p is its own auto-permission and it rejects --yolo/--auto, so its attended session launches bare (ADR-0164)\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("--why section missing\nwant contains:\n%sgot:\n%s", want, got)
		}
	}
	// The pointer to this section is not printed on the section that answers it.
	if strings.Contains(got, "run with --why") {
		t.Fatalf("--why output repeats its own pointer:\n%s", got)
	}
}

// TestTaskAgentsCatalogMarksSkippedLadderEntries pins the read surface that
// answers "why is it running the cheap model?" (ADR-0168): a ladder entry with
// an Effort model skip in force carries its remaining time, a permanent skip
// carries ∞, and every other entry renders unmarked.
func TestTaskAgentsCatalogMarksSkippedLadderEntries(t *testing.T) {
	d := &tasks.Deps{
		FS:       cmdTestFS(filepath.Join(t.TempDir(), "xdg"), ""),
		LookPath: func(file string) (string, error) { return "/mock/bin/" + file, nil },
	}
	s, _, err := d.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutAgentModelCooldown("claude", "opus", time.Now().Add(47*time.Minute+30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAgentModelCooldown("kimi", "moonshot-ai/kimi-k2.7-code-highspeed", time.Time{}); err != nil {
		t.Fatal(err)
	}
	oldLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) { return nil, nil }
	t.Cleanup(func() { taskConfigLoad = oldLoad })

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, taskAgentsOptions{}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"heavy: opus[reasoning=high] (skipped 47m) (built-in)",
		"light: moonshot-ai/kimi-k2.7-code-highspeed (skipped ∞) (built-in)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("catalog missing %q:\n%s", want, got)
		}
	}
	// A model with no skip recorded stays unmarked, and a skip is keyed by
	// preset: claude's opus skip says nothing about another preset's tier.
	if strings.Contains(got, "sonnet[reasoning=high] (skipped") {
		t.Fatalf("unskipped ladder entry marked as skipped:\n%s", got)
	}
	if strings.Contains(got, "moonshot-ai/kimi-k3[reasoning=high] (skipped") {
		t.Fatalf("kimi heavy marked from a light-tier skip:\n%s", got)
	}
}

func TestTaskAgentsCommandRegisteredAndHelpVisible(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"tasks", "agents"})
	if err != nil {
		t.Fatal(err)
	}
	if got != taskAgentsCmd {
		t.Fatalf("Find(tasks agents) = %q, want %q", got.CommandPath(), taskAgentsCmd.CommandPath())
	}

	var out bytes.Buffer
	taskCmd.SetOut(&out)
	taskCmd.SetErr(&out)
	t.Cleanup(func() {
		taskCmd.SetOut(nil)
		taskCmd.SetErr(nil)
	})
	if err := taskCmd.Help(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\n  agents ") {
		t.Fatalf("tasks help missing agents command:\n%s", out.String())
	}
}
