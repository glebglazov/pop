package dashboard

import (
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

// pollDashboard drives one real poll tick and applies every message the tick's
// batch produces, so a test sees exactly what the running dashboard sees on the
// next tick — the reload included.
func pollDashboard(t *testing.T, m QueueDashboard) QueueDashboard {
	t.Helper()
	updated, cmd := m.update(dashboardTickMsg{page: m.page.id})
	m = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("the poll tick produced no commands")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("the poll tick is no longer a batch")
	}
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		msg := sub()
		if msg == nil {
			continue
		}
		if _, isTick := msg.(dashboardTickMsg); isTick {
			// The chain's own next tick: applying it here would poll forever.
			continue
		}
		updated, _ = m.update(msg)
		m = updated.(QueueDashboard)
	}
	return m
}

// A document peeked on the dashboard follows edits to its file while the peek is
// open: the poll re-reads it, and the changed content re-renders rather than
// being read through the cached rendering of the open-time text
// (ADR-0242 decision 6).
func TestDocumentPeekFollowsEditsToItsFile(t *testing.T) {
	root := t.TempDir()
	row := genericDetailRow()
	row.DefPath = root
	setDir := filepath.Join(root, row.ID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(setDir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# Spec\n\n- open time finding\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kind := &artifactDetailKind{id: ref.KindTaskSet, artifacts: []work.Artifact{
		{Type: "spec", Name: "spec.md", Path: specPath, At: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)},
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
	if !strings.Contains(m.View().Content, "open time finding") {
		t.Fatalf("the peek does not show the file it opened:\n%s", m.View().Content)
	}

	// The rendering is cached, so a rewrite that the peek never learns about would
	// keep showing the open-time document even after a re-render at this width.
	before := m.detail.peek.body(m.width, ui.CurrentAppearance())
	if err := os.WriteFile(specPath, []byte("# Spec\n\n- edited finding\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m = pollDashboard(t, m)

	if got := m.detail.peek.text; got != "# Spec\n\n- edited finding\n" {
		t.Fatalf("the poll did not re-read the peeked file: %q", got)
	}
	after := m.detail.peek.body(m.width, ui.CurrentAppearance())
	if after == before || !strings.Contains(after, "edited finding") || strings.Contains(after, "open time finding") {
		t.Fatalf("the render cache did not invalidate on the content change:\n%s", after)
	}
	view := m.View().Content
	if !strings.Contains(view, "edited finding") || strings.Contains(view, "open time finding") {
		t.Fatalf("the peek view still shows the open-time document:\n%s", view)
	}

	// A poll that finds the file unchanged leaves the peek exactly as it is, and a
	// read failure keeps the last good document rather than replacing a readable
	// view with an error.
	m = pollDashboard(t, m)
	if got := m.detail.peek.text; got != "# Spec\n\n- edited finding\n" {
		t.Fatalf("an unchanged file disturbed the peek: %q", got)
	}
	if err := os.Remove(specPath); err != nil {
		t.Fatal(err)
	}
	m = pollDashboard(t, m)
	if got := m.detail.peek.text; got != "# Spec\n\n- edited finding\n" {
		t.Fatalf("a failed re-read discarded the last good document: %q", got)
	}
	if m.detail.peek.err != nil {
		t.Fatalf("a failed re-read turned a readable peek into an error: %v", m.detail.peek.err)
	}
}

// A re-read that lands after the reader moved on belongs to no open peek, so it
// is dropped instead of pasting one document's text under another's title.
func TestDocumentPeekRefreshForAnotherPathIsDropped(t *testing.T) {
	m := genericDetailDashboard(&itemVerbKind{})
	m.detail = m.openDetailView(genericDetailRow())
	m.detail.peek = &documentPeek{path: "/tmp/second.md", title: "second", text: "second body\n"}

	updated, _ := m.update(dashboardDocumentTextMsg{path: "/tmp/first.md", text: "first body\n", refresh: true})
	m = updated.(QueueDashboard)
	if got := m.detail.peek.text; got != "second body\n" {
		t.Fatalf("a stale path's re-read landed in the open peek: %q", got)
	}
}
