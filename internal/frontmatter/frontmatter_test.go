package frontmatter

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAllFields(t *testing.T) {
	content := "---\nschedule: every 6h\nagents:\n    - claude\n    - codex\neffort: heavy\n---\n# Body\n\nDo the thing.\n"
	f, body, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	want := Fields{Schedule: "every 6h", Agents: []string{"claude", "codex"}, Effort: "heavy"}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("fields = %+v, want %+v", f, want)
	}
	if body != "# Body\n\nDo the thing.\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseInlineList(t *testing.T) {
	// A refinement agent may write valid YAML in flow style; a real parser must
	// accept it, so we don't wrongly suspend a routine over formatting.
	f, _, err := Parse("---\nagents: [claude, codex]\n---\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Agents, []string{"claude", "codex"}) {
		t.Fatalf("agents = %v", f.Agents)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	f, body, err := Parse("no fence here\njust prose\n")
	if err != nil {
		t.Fatal(err)
	}
	if !f.IsEmpty() {
		t.Fatalf("expected empty fields, got %+v", f)
	}
	if body != "no fence here\njust prose\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseEmptyFrontmatter(t *testing.T) {
	f, body, err := Parse("---\n---\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	if !f.IsEmpty() {
		t.Fatalf("expected empty fields, got %+v", f)
	}
	if body != "body\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseUnterminated(t *testing.T) {
	if _, _, err := Parse("---\nschedule: every 6h\n"); err == nil {
		t.Fatal("expected an error for an unterminated frontmatter block")
	}
}

func TestParseInvalidYAML(t *testing.T) {
	if _, _, err := Parse("---\nagents: [unclosed\n---\nbody\n"); err == nil {
		t.Fatal("expected an error for unparseable YAML frontmatter")
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		fields Fields
		body   string
	}{
		{"all", Fields{Schedule: "every 6h", Agents: []string{"claude", "codex"}, Effort: "heavy"}, "# Body\n\ntext\n"},
		{"empty", Fields{}, "# Body\n\ntext\n"},
		{"schedule only", Fields{Schedule: "daily at 10:00"}, "prompt\n"},
		{"agents only", Fields{Agents: []string{"claude"}}, "prompt\n"},
		{"empty body", Fields{Schedule: "every 6h"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Marshal(c.fields, c.body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, "---\n") {
				t.Fatalf("marshal output missing opening fence: %q", out)
			}
			gotFields, gotBody, err := Parse(out)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotFields, c.fields) {
				t.Fatalf("round-trip fields = %+v, want %+v", gotFields, c.fields)
			}
			if gotBody != c.body {
				t.Fatalf("round-trip body = %q, want %q", gotBody, c.body)
			}
		})
	}
}

func TestMarshalEmptyOmitsKeys(t *testing.T) {
	out, err := Marshal(Fields{}, "body\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "---\n---\nbody\n" {
		t.Fatalf("empty frontmatter = %q, want a bare fence", out)
	}
}
