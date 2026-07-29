package integrate

import (
	"strings"
	"testing"
)

// TestSpawnAgentSkillRegistry pins the embedded spawn-agent skill body: one
// registry row per agent preset, auto-approve flags, remote-control shapes,
// pop pane spawn flow, default-host behaviour, and no false per-session links.
func TestSpawnAgentSkillRegistry(t *testing.T) {
	t.Parallel()
	src, err := skillFiles.ReadFile("skills/pop/spawn-agent.md")
	if err != nil {
		t.Fatalf("read embedded spawn-agent: %v", err)
	}
	body := string(src)

	for _, preset := range []string{"claude", "codex", "cursor", "pi", "opencode"} {
		if !strings.Contains(body, "`"+preset+"`") {
			t.Errorf("registry missing preset keyword %q", preset)
		}
	}

	mustContain := []string{
		"`pop pane create`",
		"pop-spawn",
		"$PANE_ID",
		"--dangerously-skip-permissions",
		"--dangerously-bypass-approvals-and-sandbox",
		"--force",
		"`--auto`",
		"/remote-control",
		"claude.ai/code",
		"codex remote-control",
		"opencode web",
		"same preset as the agent running this skill",
		"Naming a different keyword",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("spawn-agent body missing %q", want)
		}
	}

	// Remote-control honesty: no per-session link claims for agents without one.
	if strings.Contains(body, "cursor") && strings.Contains(body, "per-session") {
		if strings.Contains(body, "cursor") {
			cursorSection := body[strings.Index(body, "| cursor |"):]
			if strings.Contains(cursorSection[:min(400, len(cursorSection))], "per-session") {
				t.Error("cursor row must not claim a per-session link")
			}
		}
	}
	if strings.Contains(body, "do not report a remote-control link") == false {
		t.Error("spawn-agent must warn against false remote-control links")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
