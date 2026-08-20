package ui

import (
	"image/color"
	"os"
	"strings"
	"testing"
)

// The whole point of the appearance is that the three members draw differently,
// and that plain is a document a pipe or a pull request can hold unchanged.
func TestRenderMarkdownDrawsEachAppearanceDifferently(t *testing.T) {
	document := "# Title\n\nBody text with `code` and a [link](https://example.com).\n\n- first\n- second\n"

	plain := RenderMarkdown(document, 60, AppearancePlain)
	dark := RenderMarkdown(document, 60, AppearanceDark)
	light := RenderMarkdown(document, 60, AppearanceLight)

	for _, pair := range []struct {
		name string
		a, b string
	}{
		{"plain and dark", plain, dark},
		{"plain and light", plain, light},
		{"dark and light", dark, light},
	} {
		if pair.a == pair.b {
			t.Errorf("%s rendered identically:\n%q", pair.name, pair.a)
		}
	}

	if strings.Contains(plain, "\x1b") {
		t.Errorf("plain rendering carries an ANSI escape:\n%q", plain)
	}
	for name, rendered := range map[string]string{"dark": dark, "light": light} {
		if !strings.Contains(rendered, "\x1b") {
			t.Errorf("%s rendering carries no colour:\n%q", name, rendered)
		}
	}
}

func TestAppearanceOfBackgroundColour(t *testing.T) {
	for _, tc := range []struct {
		name string
		bg   color.Color
		want Appearance
	}{
		{"no answer", nil, AppearancePlain},
		{"near black", color.RGBA{R: 0x1d, G: 0x1f, B: 0x21, A: 0xff}, AppearanceDark},
		{"tmux-under-ghostty cream", color.RGBA{R: 0xfb, G: 0xf1, B: 0xc7, A: 0xff}, AppearanceLight},
	} {
		if got := AppearanceOf(tc.bg); got != tc.want {
			t.Errorf("AppearanceOf(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A non-terminal stdout is asked nothing at all: the query would put a terminal
// in raw mode, and there is none.
func TestResolveAppearanceOnANonTerminalIsPlain(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	if got := ResolveAppearance(os.Stdin, out); got != AppearancePlain {
		t.Errorf("ResolveAppearance(non-terminal) = %v, want plain", got)
	}
}
