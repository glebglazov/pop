package ui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour/styles"
	"golang.org/x/term"

	"github.com/glebglazov/pop/internal/tty"
)

// Appearance is the background a terminal actually has, resolved by pop rather
// than guessed by a renderer (ADR-0230). Every palette pop prints is selected
// from it.
//
// Plain is a full member, not a defensive default: glamour pins a true-colour
// profile, so an explicit light or dark style emits ANSI even into a pipe, and
// only plain keeps a redirected document byte-exact (ADR-0222).
type Appearance int

const (
	// AppearancePlain is markup with no colour — the answer whenever the
	// terminal will not say what its background is, and the answer for a
	// document that is not going to a terminal at all. It is the zero value so
	// that an unresolved appearance is the safe one.
	AppearancePlain Appearance = iota
	// AppearanceDark is a terminal whose background is darker than its text.
	AppearanceDark
	// AppearanceLight is a terminal whose background is lighter than its text.
	AppearanceLight
)

// ResolveAppearance asks the terminal what its background colour is, and answers
// plain when it cannot ask or is not answered.
//
// in and out are the files the query rides on — typically stdin and stdout. The
// query puts the terminal in raw mode, so it is refused outright unless out is a
// terminal whose foreground process group is pop's own, and it runs inside the
// SIGTTIN/SIGTTOU guard even then: raw mode from a background group would
// otherwise stop the whole group, and pop runs its own commands in panes.
func ResolveAppearance(in, out *os.File) Appearance {
	if out == nil || in == nil {
		return AppearancePlain
	}
	fd := int(out.Fd())
	if !term.IsTerminal(fd) || !tty.HoldsForeground(fd) {
		return AppearancePlain
	}
	var bg color.Color
	if err := tty.GuardRead(func() error {
		var err error
		bg, err = lipgloss.BackgroundColor(in, out)
		return err
	}); err != nil {
		return AppearancePlain
	}
	return AppearanceOf(bg)
}

// AppearanceOf reads an appearance off a background colour, which is how a
// surface that learns the colour by message rather than by query gets its
// answer. A nil colour — a terminal that answered nothing — is plain.
func AppearanceOf(bg color.Color) Appearance {
	if bg == nil {
		return AppearancePlain
	}
	if isDarkBackground(bg) {
		return AppearanceDark
	}
	return AppearanceLight
}

// isDarkBackground reports whether a colour is dark by its HSL lightness, the
// midpoint between its strongest and weakest channel.
func isDarkBackground(c color.Color) bool {
	r, g, b, a := c.RGBA()
	if a == 0 {
		// A fully transparent background says nothing about what shows through.
		return true
	}
	// RGBA returns alpha-premultiplied channels; undo that before comparing.
	red := float64(r) / float64(a)
	green := float64(g) / float64(a)
	blue := float64(b) / float64(a)
	high, low := red, red
	for _, channel := range []float64{green, blue} {
		if channel > high {
			high = channel
		}
		if channel < low {
			low = channel
		}
	}
	return (high+low)/2 < 0.5
}

// glamourStyle is the standard glamour style that draws for this appearance.
func (a Appearance) glamourStyle() string {
	switch a {
	case AppearanceDark:
		return styles.DarkStyle
	case AppearanceLight:
		return styles.LightStyle
	default:
		return styles.NoTTYStyle
	}
}
