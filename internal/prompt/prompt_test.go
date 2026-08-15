package prompt

import (
	"strings"
	"testing"
	"testing/fstest"
)

// The seam's whole job in one pass: a template with naked conditional lines
// renders through MustParseFS/MustRender to text satisfying the invariant the
// goldens are held to, with the absent section leaving no widened gap.
func TestMustRenderNormalizesConditionalSections(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"gate.tmpl.md": &fstest.MapFile{Data: []byte(
			"Task set: {{.SetID}}   \n" +
				"\n" +
				"{{if .Note}}Note: {{.Note}}\n" +
				"{{end}}\n" +
				"Allowed outcomes:\n" +
				"{{range .Outcomes}}- {{.}}\n" +
				"{{end}}\n\n\n",
		)},
	}
	tmpl := MustParseFS(fsys, "*.tmpl.md")

	view := struct {
		SetID    string
		Note     string
		Outcomes []string
	}{SetID: "demo", Outcomes: []string{"accept", "exit"}}

	got := MustRender(tmpl, "gate.tmpl.md", view)
	want := "Task set: demo\n\nAllowed outcomes:\n- accept\n- exit\n"
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}

	view.Note = "already adjudicated"
	withNote := MustRender(tmpl, "gate.tmpl.md", view)
	if !strings.Contains(withNote, "Note: already adjudicated\n\nAllowed outcomes:") {
		t.Fatalf("note section did not render into a single blank-line gap:\n%q", withNote)
	}
}

// A degraded prompt is worse than a crash: an execute failure must not reach the
// agent as a truncated briefing.
func TestMustRenderPanicsOnExecuteError(t *testing.T) {
	t.Parallel()
	tmpl := MustParseFS(fstest.MapFS{
		"boom.tmpl.md": &fstest.MapFile{Data: []byte("{{.Missing.Field}}\n")},
	}, "*.tmpl.md")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustRender returned instead of panicking on an execute error")
		}
		if !strings.Contains(r.(string), "boom.tmpl.md") {
			t.Fatalf("panic does not name the template: %v", r)
		}
	}()
	_ = MustRender(tmpl, "boom.tmpl.md", struct{}{})
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "collapses blank runs", in: "a\n\n\n\nb\n", want: "a\n\nb\n"},
		{name: "strips trailing whitespace", in: "a   \nb\t\n", want: "a\nb\n"},
		{name: "adds the missing trailing newline", in: "a", want: "a\n"},
		{name: "trims to one trailing newline", in: "a\n\n\n", want: "a\n"},
		{name: "keeps a single blank line", in: "a\n\nb\n", want: "a\n\nb\n"},
		{name: "blank text is empty", in: "\n  \n\t\n", want: ""},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Normalize(tc.in); got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
