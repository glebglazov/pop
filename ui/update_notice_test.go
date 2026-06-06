package ui

import (
	"strings"
	"testing"
)

func TestRenderUpdateNotice_RightAlignedAndDimmed(t *testing.T) {
	out := renderUpdateNotice(40, "update available: 2026.6.1")
	plain := StripANSI(out)

	if !strings.HasSuffix(plain, "update available: 2026.6.1") {
		t.Errorf("notice should be right-aligned, got %q", plain)
	}
	if len([]rune(plain)) != 40 {
		t.Errorf("notice width = %d, want 40", len([]rune(plain)))
	}
	if out == plain {
		t.Errorf("notice should carry dim styling ANSI codes, got plain %q", out)
	}
}

func TestPickerViewRendersUpdateNotice(t *testing.T) {
	items := []Item{{Name: "alpha", Path: "/a"}, {Name: "beta", Path: "/b"}}
	picker := NewPicker(items, WithUpdateNotice("update available: 2026.6.1"))
	picker.width = 50
	picker.height = 10

	view := picker.viewProject()
	plain := StripANSI(view)
	lines := strings.Split(plain, "\n")

	if !strings.Contains(lines[0], "update available: 2026.6.1") {
		t.Errorf("first line should carry the notice, got %q", lines[0])
	}
	if !strings.Contains(plain, "alpha") || !strings.Contains(plain, "beta") {
		t.Errorf("list items should still render alongside the notice")
	}
}

func TestPickerViewNoNoticeByDefault(t *testing.T) {
	items := []Item{{Name: "alpha", Path: "/a"}}
	picker := NewPicker(items)
	picker.width = 50
	picker.height = 10

	plain := StripANSI(picker.viewProject())
	if strings.Contains(plain, "update available") {
		t.Errorf("no notice should render when none is set")
	}
}

func TestDashboardViewRendersUpdateNotice(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "proj", Name: "proj (%1)", Status: AttentionUnread},
	}
	d := NewDashboard(panes, AttentionCallbacks{}, nil, WithDashboardUpdateNotice("update available: 2026.6.1"))
	d.width = 80
	d.height = 12

	view := d.viewDashboard()
	plain := StripANSI(view)
	lines := strings.Split(plain, "\n")

	if !strings.Contains(lines[0], "update available: 2026.6.1") {
		t.Errorf("dashboard first line should carry the notice, got %q", lines[0])
	}
}

func TestDashboardEmptyViewRendersUpdateNotice(t *testing.T) {
	d := NewDashboard(nil, AttentionCallbacks{}, nil, WithDashboardUpdateNotice("update available: 2026.6.1"))
	d.width = 80
	d.height = 12

	plain := StripANSI(d.viewDashboard())
	lines := strings.Split(plain, "\n")
	if !strings.Contains(lines[0], "update available: 2026.6.1") {
		t.Errorf("empty dashboard should still carry the notice on the first line, got %q", lines[0])
	}
}
