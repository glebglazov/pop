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
			name:  "block reserves N lines",
			frame: Frame{Block: []string{"filters", "show done", "show archived"}},
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
				Block:    []string{"filters", "show done"},
				Hints:    "  Esc back",
			},
			termH: 20,
			// 20 - 1 (notice) - 1 (header) - 3 (input box) - 2 (warnings) - 1 (status) - 2 (block) - 1 (hints) = 9
			want: 9,
		},
		{
			name: "yields floor on a short terminal so chrome fits",
			frame: Frame{
				Notice:   "Update available",
				Header:   "Projects",
				InputBox: "> ",
				Warnings: []string{"one", "two", "three"},
				Hints:    "  Esc back",
			},
			termH: 10,
			// 10 - 1 - 1 - 3 - 3 - 1 = 1; flooring to 3 would push past TermH
			want: 1,
		},
		{
			name:  "yields floor on a tiny terminal",
			frame: Frame{},
			termH: 1,
			want:  1,
		},
		{
			name: "clips block so body floor still fits",
			frame: Frame{
				Header:    "Work",
				Subheader: "agent",
				Status:    "ok",
				Footnote:  "skip",
				Block:     []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
				Hints:     "  q",
			},
			termH: 16,
			// fixed chrome 5 + body floor 3 = 8; block clipped to 8 → body 3
			want: 3,
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
		Block:    []string{"filters", "show done"},
		Hints:    "  Esc back",
	}

	out := f.Render("BODY")

	notice := indexOf(t, out, "Update available")
	header := indexOf(t, out, "Projects")
	body := indexOf(t, out, "BODY")
	inputBox := indexOf(t, out, "Help")
	warning := indexOf(t, out, "low disk space")
	status := indexOf(t, out, "Copied to clipboard")
	block := indexOf(t, out, "filters")
	hints := indexOf(t, out, "Esc back")

	if !(notice < header && header < body && body < inputBox && inputBox < warning && warning < status && status < block && block < hints) {
		t.Fatalf("regions out of order: notice=%d header=%d body=%d inputBox=%d warning=%d status=%d block=%d hints=%d",
			notice, header, body, inputBox, warning, status, block, hints)
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

// TestFrameRenderLeavesFullBodyUnchanged: a body that exactly fills the budget
// renders byte-identical to the no-height (unpadded) path.
func TestFrameRenderLeavesFullBodyUnchanged(t *testing.T) {
	base := Frame{Width: 20, TermH: 20, Header: "H", Status: "S", Hints: "  q"}
	budget := base.BodyHeight(20)

	rows := make([]string, budget)
	for i := range rows {
		rows[i] = fmt.Sprintf("row%d", i)
	}
	body := strings.Join(rows, "\n")

	unpadded := base
	unpadded.TermH = 0
	if got, want := base.Render(body), unpadded.Render(body); got != want {
		t.Fatalf("exact-fit body of %d lines: padded render differs from unpadded\n got=%q\nwant=%q",
			budget, got, want)
	}
}

// TestFrameRenderTruncatesOverfullBody: a body longer than the budget is cut so
// the frame still fits in TermH (the symmetric half of padBody's short-body pad).
func TestFrameRenderTruncatesOverfullBody(t *testing.T) {
	f := Frame{Width: 20, TermH: 20, Header: "H", Status: "S", Hints: "  q"}
	budget := f.BodyHeight(20)
	rows := make([]string, budget+5)
	for i := range rows {
		rows[i] = fmt.Sprintf("row%d", i)
	}
	out := f.Render(strings.Join(rows, "\n"))
	lines := strings.Split(out, "\n")
	if len(lines) != 20 {
		t.Fatalf("got %d lines, want 20", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "q") {
		t.Fatalf("last line = %q, want hints", lines[len(lines)-1])
	}
	if strings.Contains(out, fmt.Sprintf("row%d", budget+4)) {
		t.Fatalf("overfull body rows survived truncation:\n%s", out)
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
				Block:    []string{"filters", "show done"},
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

// TestFrameRenderNeverExceedsTermH is the ADR-0197 overflow spine: across a
// sweep of region combinations and block heights, Render's line count never
// exceeds TermH when TermH is known.
func TestFrameRenderNeverExceedsTermH(t *testing.T) {
	termHeights := []int{8, 12, 16, 20, 24}
	blockHeights := []int{0, 1, 2, 5, 10, 20}
	type opts struct {
		notice, header, subheader, input, status, footnote, hints bool
		warnings                                                    int
	}
	var cases []opts
	for _, notice := range []bool{false, true} {
		for _, header := range []bool{false, true} {
			for _, subheader := range []bool{false, true} {
				for _, input := range []bool{false, true} {
					for _, status := range []bool{false, true} {
						for _, footnote := range []bool{false, true} {
							for _, hints := range []bool{false, true} {
								for _, warnings := range []int{0, 1, 2} {
									cases = append(cases, opts{
										notice: notice, header: header, subheader: subheader,
										input: input, status: status, footnote: footnote,
										hints: hints, warnings: warnings,
									})
								}
							}
						}
					}
				}
			}
		}
	}

	for _, termH := range termHeights {
		for _, blockH := range blockHeights {
			for _, c := range cases {
				f := Frame{Width: 40, TermH: termH}
				if c.notice {
					f.Notice = "Update available"
				}
				if c.header {
					f.Header = "Header"
				}
				if c.subheader {
					f.Subheader = "Subheader"
				}
				if c.input {
					f.InputBox = "> "
				}
				if c.warnings > 0 {
					f.Warnings = make([]string, c.warnings)
					for i := range f.Warnings {
						f.Warnings[i] = fmt.Sprintf("warn%d", i)
					}
				}
				if c.status {
					f.Status = "status"
				}
				if c.footnote {
					f.Footnote = "footnote"
				}
				if blockH > 0 {
					f.Block = make([]string, blockH)
					for i := range f.Block {
						f.Block[i] = fmt.Sprintf("block-%d", i)
					}
				}
				if c.hints {
					f.Hints = "  esc close"
				}

				out := f.Render("body")
				lines := strings.Split(out, "\n")
				if len(lines) > termH {
					t.Fatalf("TermH=%d block=%d opts=%+v: Render produced %d lines:\n%s",
						termH, blockH, c, len(lines), out)
				}
				if c.hints && !strings.Contains(lines[len(lines)-1], "esc close") {
					t.Fatalf("TermH=%d block=%d: hints lost from last line %q",
						termH, blockH, lines[len(lines)-1])
				}
			}
		}
	}
}

// TestFrameRenderClipsBlockAtTermH16 is the measured reproduction: TermH=16 with
// header+subheader+status+footnote+hints and a ten-line block used to paint 18
// lines. After the fix it fits in 16, the block cut is visible, and hints survive.
func TestFrameRenderClipsBlockAtTermH16(t *testing.T) {
	block := make([]string, 10)
	for i := range block {
		block[i] = fmt.Sprintf("block-%d", i)
	}
	f := Frame{
		Width:     40,
		TermH:     16,
		Header:    "Work · active · 1 here",
		Subheader: "agent: cursor",
		Status:    "selected",
		Footnote:  "skipped: none",
		Block:     block,
		Hints:     "j/k move · 1-9/enter select · esc close",
	}
	out := f.Render("BODY")
	lines := strings.Split(out, "\n")
	if len(lines) != 16 {
		t.Fatalf("got %d lines, want 16:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[len(lines)-1], "esc close") {
		t.Fatalf("last line lost hints: %q", lines[len(lines)-1])
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "clipped to fit the pane") {
		t.Fatalf("clipped block missing visible cut marker:\n%s", out)
	}
	if strings.Contains(joined, "block-9") {
		t.Fatalf("full block still present after clip:\n%s", out)
	}
	if f.BodyHeight(16) != 3 {
		t.Fatalf("BodyHeight(16) = %d, want 3", f.BodyHeight(16))
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
