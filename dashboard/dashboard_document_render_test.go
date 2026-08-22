package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Document peek renders markdown and shows every other extension raw, and its
// scrolling counts rendered lines rather than the file's own (ADR-0222).
func TestDocumentPeekRendersMarkdownByExtensionAndScrollsRenderedLines(t *testing.T) {
	root := t.TempDir()
	row := genericDetailRow()
	row.DefPath = root
	setDir := filepath.Join(root, row.ID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// One long paragraph line, so the rendered body has more lines than the file.
	spec := "# Spec\n\n" + strings.Repeat("prose that wraps ", 160) + "\n\n- first finding\n- second finding\n"
	progress := "12:00 [01-task.md] done\n\n---\n\n12:30 [02-task.md] done\n"
	specPath := filepath.Join(setDir, "spec.md")
	progressPath := filepath.Join(setDir, "progress.txt")
	for path, body := range map[string]string{specPath: spec, progressPath: progress} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	kind := &artifactDetailKind{id: ref.KindTaskSet, artifacts: []work.Artifact{
		{Type: "spec", Name: "spec.md", Path: specPath, At: at},
		{Type: "progress", Name: "progress.txt", Path: progressPath, At: at},
	}}
	m := openArtifactDetail(t, artifactDetailDashboard(kind, row))
	updated, _ := m.update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = updated.(QueueDashboard)

	openPeek := func(t *testing.T, m QueueDashboard) QueueDashboard {
		t.Helper()
		updated, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(QueueDashboard)
		if cmd == nil {
			t.Fatal("Enter did not open the Document peek")
		}
		updated, _ = m.update(cmd())
		return updated.(QueueDashboard)
	}

	m = openPeek(t, m)
	body := m.detail.peek.body(m.width, ui.CurrentAppearance())
	if strings.Contains(body, "- first finding") || !strings.Contains(body, "first finding") {
		t.Fatalf("markdown document was not rendered:\n%q", body)
	}
	if !strings.Contains(m.View().Content, "prose that wraps") {
		t.Fatalf("peek view does not show the rendered document:\n%s", m.View().Content)
	}
	if m.detail.peek.text != spec {
		t.Fatalf("peek lost the file's own text: %q", m.detail.peek.text)
	}

	// Scrolling, paging and G all move over the rendered lines, which the wrapped
	// paragraph makes more numerous than the file's.
	rendered := m.detail.peek.lines(m.width, ui.CurrentAppearance())
	if len(rendered) <= len(documentPeekLines(spec)) {
		t.Fatalf("rendered body has %d lines, want more than the file's %d", len(rendered), len(documentPeekLines(spec)))
	}
	updated, _ = m.update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	m = updated.(QueueDashboard)
	if want := len(rendered) - m.documentPeekPageSize(); m.detail.peek.scroll != want {
		t.Fatalf("G scrolled to %d, want the rendered bottom %d", m.detail.peek.scroll, want)
	}

	// A resize re-wraps: a narrower window yields more rendered lines.
	wide := len(rendered)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 18})
	m = updated.(QueueDashboard)
	if narrow := len(m.detail.peek.lines(m.width, ui.CurrentAppearance())); narrow <= wide {
		t.Fatalf("resize did not re-wrap: %d lines at width 50, %d at width 120", narrow, wide)
	}

	// The progress record is not markdown, so its rule separators survive.
	updated, _ = m.update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = updated.(QueueDashboard)
	updated, _ = m.update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = openPeek(t, updated.(QueueDashboard))
	if got := m.detail.peek.body(m.width, ui.CurrentAppearance()); got != progress {
		t.Fatalf("progress record was not shown raw:\n%q", got)
	}
}

// The Document peek's page is the lines it really draws. Chrome the peek grows
// takes those lines away, so an item menu open over the document must shorten
// the page with it — otherwise the peek pages past its own bottom and G leaves
// the last lines unreachable.
func TestDocumentPeekPageShrinksWithTheMenuOverIt(t *testing.T) {
	root := t.TempDir()
	row := genericDetailRow()
	row.DefPath = root
	setDir := filepath.Join(root, row.ID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var doc strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&doc, "line %02d\n", i)
	}
	docPath := filepath.Join(setDir, "notes.txt")
	if err := os.WriteFile(docPath, []byte(doc.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	kind := &artifactDetailKind{id: ref.KindTaskSet, artifacts: []work.Artifact{
		{Type: "progress", Name: "notes.txt", Path: docPath, At: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)},
	}}
	m := openArtifactDetail(t, artifactDetailDashboard(kind, row))
	updated, _ := m.update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = updated.(QueueDashboard)
	updated, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("Enter did not open the Document peek")
	}
	updated, _ = m.update(cmd())
	m = updated.(QueueDashboard)

	bare := m.documentPeekPageSize()

	updated, _ = m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	if m.itemMenu == nil || !m.itemMenu.inPeek {
		t.Fatal("a did not open the item menu over the peek")
	}
	withMenu := m.documentPeekPageSize()
	if withMenu >= bare {
		t.Fatalf("page over the menu = %d, want fewer than the bare %d", withMenu, bare)
	}

	// The document's bottom stays reachable: the last scroll the peek allows
	// itself must put the last line on screen. An over-long page understates the
	// scroll and strands the tail off screen.
	m.detail.peek.scroll = m.maxDocumentPeekScroll()
	if !strings.Contains(ui.StripANSI(m.View().Content), "line 60") {
		t.Fatalf("the peek's last scroll does not reach the last line:\n%s", m.View().Content)
	}
}
