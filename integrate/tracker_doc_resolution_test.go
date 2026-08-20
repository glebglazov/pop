package integrate

import (
	"strings"
	"testing"
)

// trackerDocSkills are the shipped skills that must read the issue-tracker doc
// before they can publish anything.
var trackerDocSkills = []string{
	"skills/pop/to-tasks/SKILL.md",
	"skills/pop/to-spec/SKILL.md",
	"skills/pop/wayfinder/SKILL.md",
}

// TestSkills_ResolveTrackerDocThroughPop pins how a skill reaches the
// issue-tracker doc once ADR-0226 retired the user-level
// `~/.agents/docs/issue-tracker.md` symlink: through
// `pop conventions get issue-tracker`, which always answers. A skill still
// reading that path by hand would halt on a machine where the link is gone —
// which, after the retirement, is every machine.
func TestSkills_ResolveTrackerDocThroughPop(t *testing.T) {
	t.Parallel()
	for _, name := range trackerDocSkills {
		body, err := skillFiles.ReadFile(name)
		if err != nil {
			t.Fatalf("read embedded skill %s: %v", name, err)
		}
		text := string(body)
		if !strings.Contains(text, "pop conventions get issue-tracker") {
			t.Errorf("%s does not resolve the tracker doc through pop", name)
		}
		for _, retired := range []string{
			"~/.agents/docs/issue-tracker.md",
			"there is no further fallback",
		} {
			if strings.Contains(text, retired) {
				t.Errorf("%s still resolves the tracker doc the retired way: %q", name, retired)
			}
		}
	}
}
