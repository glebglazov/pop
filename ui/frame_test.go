package ui

import "testing"

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
				Hints:    "  Esc back",
			},
			termH: 20,
			// 20 - 1 (notice) - 1 (header) - 3 (input box) - 2 (warnings) - 1 (hints) = 12
			want: 12,
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
		Hints:    "  Esc back",
	}

	out := f.Render("BODY")

	notice := indexOf(t, out, "Update available")
	header := indexOf(t, out, "Projects")
	body := indexOf(t, out, "BODY")
	inputBox := indexOf(t, out, "Help")
	warning := indexOf(t, out, "low disk space")
	hints := indexOf(t, out, "Esc back")

	if !(notice < header && header < body && body < inputBox && inputBox < warning && warning < hints) {
		t.Fatalf("regions out of order: notice=%d header=%d body=%d inputBox=%d warning=%d hints=%d",
			notice, header, body, inputBox, warning, hints)
	}
}

func TestFrameRenderOmitsAbsentRegions(t *testing.T) {
	f := Frame{Width: 20}
	out := f.Render("BODY")

	if out != "BODY" {
		t.Fatalf("Render() with no regions = %q, want %q", out, "BODY")
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
