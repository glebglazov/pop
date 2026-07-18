package ui

import (
	"fmt"
	"strings"
	"testing"
)

func TestFrameBodyHeight(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
		termH int
		want  int
	}{
		{
			name:  "no regions",
			frame: Frame{},
			termH: 20,
			want:  20,
		},
		{
			name:  "notice reserves one line",
			frame: Frame{Notice: "Update available"},
			termH: 20,
			want:  19,
		},
		{
			name:  "header reserves one line",
			frame: Frame{Header: "Projects"},
			termH: 20,
			want:  19,
		},
		{
			name:  "input box reserves three lines",
			frame: Frame{InputBox: "> "},
			termH: 20,
			want:  17,
		},
		{
			name:  "status reserves one line",
			frame: Frame{Status: "Copied to clipboard"},
			termH: 20,
			want:  19,
		},
		{
			name:  "empty status reserves nothing",
			frame: Frame{Status: ""},
			termH: 20,
			want:  20,
		},
		{
			name:  "hints reserve one line",
			frame: Frame{Hints: "  Esc back"},
			termH: 20,
			want:  19,
		},
		{
			name:  "warnings reserve N lines",
			frame: Frame{Warnings: []string{"one", "two", "three"}},
			termH: 20,
			want:  17,
		},
		{
			name: "all regions combine",
			frame: Frame{
				Notice:   "Update available",
				Header:   "Projects",
				InputBox: "> ",
				Warnings: []string{"one", "two"},
				Status:   "Copied",
				Hints:    "  Esc back",
			},
			termH: 20,
			// 20 - 1 (notice) - 1 (header) - 3 (input box) - 2 (warnings) - 1 (status) - 1 (hints) = 11
			want: 11,
		},
		{
			name: "floors at 3 on a short terminal",
			frame: Frame{
				Notice:   "Update available",
				Header:   "Projects",
				InputBox: "> ",
				Warnings: []string{"one", "two", "three"},
				Hints:    "  Esc back",
			},
			termH: 10,
			// 10 - 1 - 1 - 3 - 3 - 1 = 1, floored to 3
			want: 3,
		},
		{
			name:  "floors at 3 with no regions on a tiny terminal",
			frame: Frame{},
			termH: 1,
			want:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.frame.BodyHeight(tt.termH)
			if got != tt.want {
				t.Errorf("BodyHeight(%d) = %d, want %d", tt.termH, got, tt.want)
			}
		})
	}
}

func TestFrameRenderOrderAndOmission(t *testing.T) {
	f := Frame{
		Width:    20,
		Notice:   "Update available",
		Header:   "Projects",
		InputBox: " Help",
		Warnings: []string{"low disk space"},
		Status:   "Copied to clipboard",
		Hints:    "  Esc back",
	}

	out := f.Render("BODY")

	notice := indexOf(t, out, "Update available")
	header := indexOf(t, out, "Projects")
	body := indexOf(t, out, "BODY")
	inputBox := indexOf(t, out, "Help")
	warning := indexOf(t, out, "low disk space")
	status := indexOf(t, out, "Copied to clipboard")
	hints := indexOf(t, out, "Esc back")

	if !(notice < header && header < body && body < inputBox && inputBox < warning && warning < status && status < hints) {
		t.Fatalf("regions out of order: notice=%d header=%d body=%d inputBox=%d warning=%d status=%d hints=%d",
			notice, header, body, inputBox, warning, status, hints)
	}
}

func TestFrameRenderOmitsAbsentStatus(t *testing.T) {
	f := Frame{Width: 20, Hints: "  Esc back"}
	out := f.Render("BODY")

	// With no Status set, only body and hints render — no status line between.
	if out != "BODY\n"+hintStyle.Render("  Esc back") {
		t.Fatalf("Render() with absent status = %q", out)
	}
}

func TestFrameRenderOmitsAbsentRegions(t *testing.T) {
	f := Frame{Width: 20}
	out := f.Render("BODY")

	if out != "BODY" {
		t.Fatalf("Render() with no regions = %q, want %q", out, "BODY")
	}
}

// TestFrameRenderPadsShortBody: a short body under a known TermH is padded so
// the hints land on the very bottom row and the body content stays under the
// header (the routine dashboard's empty-list case).
func TestFrameRenderPadsShortBody(t *testing.T) {
	f := Frame{Width: 20, TermH: 20, Header: "Routines · 0", Hints: "  h/esc quit"}
	out := f.Render("  no routines")

	lines := strings.Split(out, "\n")
	if len(lines) != 20 {
		t.Fatalf("got %d lines, want 20 (padded to full terminal height)", len(lines))
	}
	if !strings.Contains(lines[1], "no routines") {
		t.Fatalf("line 1 = %q, want body hint directly under header", lines[1])
	}
	if !strings.Contains(lines[len(lines)-1], "h/esc quit") {
		t.Fatalf("last line = %q, want hints on the bottom row", lines[len(lines)-1])
	}
}

// TestFrameRenderLeavesFullBodyUnchanged: a body that exactly fills or overfills
// the budget renders byte-identical to the no-height (unpadded) path.
func TestFrameRenderLeavesFullBodyUnchanged(t *testing.T) {
	base := Frame{Width: 20, TermH: 20, Header: "H", Status: "S", Hints: "  q"}
	budget := base.BodyHeight(20)

	for _, extra := range []int{0, 5} { // exact-fit and overfull
		rows := make([]string, budget+extra)
		for i := range rows {
			rows[i] = fmt.Sprintf("row%d", i)
		}
		body := strings.Join(rows, "\n")

		unpadded := base
		unpadded.TermH = 0
		if got, want := base.Render(body), unpadded.Render(body); got != want {
			t.Fatalf("body of %d lines (budget %d): padded render differs from unpadded\n got=%q\nwant=%q",
				budget+extra, budget, got, want)
		}
	}
}

// TestFrameRenderPadBudgetTracksRegions: the padding budget subtracts every
// present region, so a short body plus all chrome still totals exactly TermH —
// whether all regions are present or only the minimal ones.
func TestFrameRenderPadBudgetTracksRegions(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
	}{
		{name: "minimal regions", frame: Frame{Width: 20, TermH: 20, Hints: "  q"}},
		{
			name: "all regions",
			frame: Frame{
				Width:    20,
				TermH:    20,
				Notice:   "Update available",
				Header:   "Header",
				InputBox: "> ",
				Warnings: []string{"warn"},
				Status:   "Copied",
				Hints:    "  q",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.frame.Render("  short body")
			lines := strings.Split(out, "\n")
			if len(lines) != 20 {
				t.Fatalf("got %d lines, want 20 (short body padded to full height)", len(lines))
			}
			if !strings.Contains(lines[len(lines)-1], "q") {
				t.Fatalf("last line = %q, want hints on the bottom row", lines[len(lines)-1])
			}
		})
	}
}

func indexOf(t *testing.T, haystack, needle string) int {
	t.Helper()
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	t.Fatalf("expected %q to contain %q", haystack, needle)
	return -1
}
