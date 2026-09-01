package integrate

import (
	"io/fs"
	"strings"
	"testing"
)

// TestSkillsNamingTheResolveCommandForbidTheShortenedRead pins ADR-0250's
// second half. It walks every embedded skill body rather than checking today's
// call sites by name: the habit it guards is tool-shaped and belongs to whoever
// runs the command next, so a seventh skill that names the command must not be
// able to arrive without the direction.
func TestSkillsNamingTheResolveCommandForbidTheShortenedRead(t *testing.T) {
	t.Parallel()

	// The direction names the tools because the shortened read is performed with
	// them; a body that only said "read it all" leaves the pipe looking allowed.
	required := []string{
		"do not pipe it through",
		"`head`",
		"`tail`",
		"`sed -n`",
		"`grep`",
	}

	found := 0
	err := fs.WalkDir(skillFiles, "skills/pop", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, err := skillFiles.ReadFile(path)
		if err != nil {
			t.Fatalf("read embedded %s: %v", path, err)
		}
		text := string(body)
		if !strings.Contains(text, "pop conventions get") {
			return nil
		}
		found++
		for _, want := range required {
			if !strings.Contains(text, want) {
				t.Errorf("%s names `pop conventions get` without the no-shortened-read direction: missing %q", path, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded skills: %v", err)
	}
	if found == 0 {
		t.Fatal("no embedded skill names `pop conventions get`, so the walk proves nothing")
	}
}
