package conventions

import (
	"bytes"
	"strings"
	"testing"
)

// TestRenderStackEmptyNamesEveryPlaceItLooked pins the miss rendering, which is
// the only output a caller gets before pop exits 1: the four paths, in rank
// order, so the reader knows where an answer would go.
func TestRenderStackEmptyNamesEveryPlaceItLooked(t *testing.T) {
	stack := Stack{Kind: KindCommits, Layers: []Layer{
		{Origin: OriginUserDefaults, Path: "/h/.agents/docs/commits.md"},
		{Origin: OriginMemory, Path: "/d/pop/repos/pop-abc/conventions/commits.md"},
		{Origin: OriginRepository, Path: "/r/docs/agents/commits.md"},
		{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md"},
	}}

	var out bytes.Buffer
	if err := RenderStack(&out, stack); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "EMPTY") {
		t.Errorf("empty stack is not marked empty:\n%s", got)
	}
	for _, l := range stack.Layers {
		if !strings.Contains(got, l.Path) {
			t.Errorf("consulted path %q missing:\n%s", l.Path, got)
		}
	}
	// A table, not prose: the house plain-text style puts an uppercase header
	// over it.
	if !strings.Contains(got, "ORIGIN") || !strings.Contains(got, "PATH") {
		t.Errorf("consulted-paths table has no uppercase header:\n%s", got)
	}
}

// TestProvenanceDisclosesTopLayerAndMemoryOrigin pins the one line every skill
// surfaces verbatim, which is the whole reason pop emits it rather than letting
// each skill phrase it.
func TestProvenanceDisclosesTopLayerAndMemoryOrigin(t *testing.T) {
	tests := []struct {
		name   string
		layers []Layer
		want   []string
		absent []string
	}{
		{
			name: "memory on top with full frontmatter",
			layers: []Layer{
				{Origin: OriginMemory, Path: "/d/conventions/commits.md", Present: true, Body: "b",
					DerivedFrom: "a sample of 40 commits", DerivedAt: "2026-08-01"},
			},
			want: []string{"pop memory", "/d/conventions/commits.md", "a sample of 40 commits", "2026-08-01"},
		},
		{
			name: "memory under the repository's document",
			layers: []Layer{
				{Origin: OriginMemory, Path: "/d/conventions/commits.md", Present: true, Body: "b", DerivedFrom: "a sample of 40 commits"},
				{Origin: OriginRepository, Path: "/r/docs/agents/commits.md", Present: true, Body: "b"},
			},
			want: []string{"repository", "/r/docs/agents/commits.md", "a sample of 40 commits"},
		},
		{
			name: "memory without frontmatter still discloses itself",
			layers: []Layer{
				{Origin: OriginMemory, Path: "/d/conventions/commits.md", Present: true, Body: "b"},
			},
			want: []string{"pop memory", "records no derivation"},
		},
		{
			name: "no memory in the stack",
			layers: []Layer{
				{Origin: OriginRepository, Path: "/r/docs/agents/commits.md", Present: true, Body: "b"},
			},
			want:   []string{"repository"},
			absent: []string{"Pop memory"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := Stack{Kind: KindCommits, Layers: tt.layers}.Provenance()
			if strings.Contains(line, "\n") {
				t.Fatalf("provenance is not one line: %q", line)
			}
			for _, want := range tt.want {
				if !strings.Contains(line, want) {
					t.Errorf("provenance %q missing %q", line, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(line, absent) {
					t.Errorf("provenance %q should not mention %q", line, absent)
				}
			}
		})
	}
}

// TestSplitFrontmatterOnlyTakesAWellFormedFence: a document that merely opens
// with a horizontal rule is prose, and swallowing it would silently delete a
// convention.
func TestSplitFrontmatterOnlyTakesAWellFormedFence(t *testing.T) {
	fields, body := splitFrontmatter("---\nderived_from: git log\n---\nthe convention\n")
	if fields["derived_from"] != "git log" {
		t.Errorf("derived_from = %q", fields["derived_from"])
	}
	if strings.TrimSpace(body) != "the convention" {
		t.Errorf("body = %q", body)
	}

	unterminated := "---\nnot really frontmatter\nthe convention\n"
	if fields, body := splitFrontmatter(unterminated); len(fields) != 0 || body != unterminated {
		t.Errorf("unterminated fence was treated as frontmatter: %v / %q", fields, body)
	}
}
