package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAgentEntryTableAndStringForms covers ADR-0194 decision 5: every group
// takes a table entry with display_name/cmd, and a bare string is sugar for an
// entry whose cmd is that string.
func TestAgentEntryTableAndStringForms(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[work.implement]
agents = ["codex", { display_name = "Claude Usual", cmd = "claude --model opus" }]

[work.verify]
agents = [{ cmd = "codex --model gpt-5.5" }]

[work.routine]
agents = ["claude"]

[work.attended]
agents = [{ display_name = "Cursor Fast", cmd = "cursor --model composer-2.5-fast" }, "claude"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Findings) != 0 {
		t.Fatalf("findings = %#v, want none", cfg.Findings)
	}

	wantImplement := AgentEntries{
		{Cmd: "codex"},
		{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
	}
	if got := cfg.ImplementAgentEntries(); !reflect.DeepEqual(got, wantImplement) {
		t.Fatalf("implement entries = %#v, want %#v", got, wantImplement)
	}
	if got, want := cfg.VerifyAgents(), []string{"codex --model gpt-5.5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("verify commands = %#v, want %#v", got, want)
	}
	if got, want := cfg.RoutineAgents(), []string{"claude"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("routine commands = %#v, want %#v", got, want)
	}
	wantAttended := AgentEntries{
		{DisplayName: "Cursor Fast", Cmd: "cursor --model composer-2.5-fast"},
		{Cmd: "claude"},
	}
	if got := cfg.AttendedAgentEntries(); !reflect.DeepEqual(got, wantAttended) {
		t.Fatalf("attended entries = %#v, want %#v", got, wantAttended)
	}
}

// TestAgentEntryTableBlocksMatchInline pins that the [[…]] array-of-tables form
// decodes identically to the same entries written inline — the decoder hands
// back a different Go shape for each.
func TestAgentEntryTableBlocksMatchInline(t *testing.T) {
	blocks, err := Load(writeConfig(t, `
[[work.attended.agents]]
display_name = "Claude Usual"
cmd = "claude --model opus"

[[work.attended.agents]]
cmd = "cursor"
`))
	if err != nil {
		t.Fatal(err)
	}
	inline, err := Load(writeConfig(t, `
[work.attended]
agents = [{ display_name = "Claude Usual", cmd = "claude --model opus" }, { cmd = "cursor" }]
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := blocks.AttendedAgentEntries(), inline.AttendedAgentEntries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("[[work.attended.agents]] blocks = %#v, want %#v", got, want)
	}
}

// TestAgentEntryMalformedIsAFinding covers ADR-0054 tolerance: a bad entry is
// reported with its group and position, the load still succeeds, and the good
// entries around it survive.
func TestAgentEntryMalformedIsAFinding(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[work.attended]
agents = ["claude", { display_name = "No Command" }, 7, "cursor"]
`))
	if err != nil {
		t.Fatalf("malformed entry aborted the load: %v", err)
	}
	if got, want := cfg.AttendedAgents(), []string{"claude", "cursor"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("attended commands = %#v, want %#v", got, want)
	}
	wantPaths := []string{"work.attended.agents[1]", "work.attended.agents[2]"}
	var gotPaths []string
	for _, f := range cfg.Findings {
		if strings.HasPrefix(f.Path, "work.attended.agents[") {
			gotPaths = append(gotPaths, f.Path)
			if !strings.Contains(f.Message, "[work.attended]") {
				t.Fatalf("finding %q does not name its group: %s", f.Path, f.Message)
			}
		}
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("finding paths = %#v, want %#v", gotPaths, wantPaths)
	}
}
