// Package shipped holds pop's own answer for each Convention kind — the bottom
// rank of the Convention stack, embedded in the binary and displaced whole by
// any written rank above it (ADR-0226 decision 1).
//
// It is a leaf package rather than a directory of the conventions package
// because one of its documents is also a Shipped asset `pop integrate` writes
// to disk. Both readers need the same bytes, and integrate cannot import
// conventions without dragging in the Task storage tree behind it.
package shipped

import (
	"embed"
	"strings"
)

// The answers are authored as markdown a human can read and edit without
// touching Go (ADR-0208).
//
//go:embed *.md
var docs embed.FS

// Body returns pop's shipped answer for the named kind. It takes a string
// rather than a conventions.Kind because the kind enum lives in the package
// that consumes this one.
func Body(kind string) (string, bool) {
	raw, err := docs.ReadFile(kind + ".md")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

// IssueTrackerDoc is pop's tracker document as bytes. It is the `issue-tracker`
// shipped answer and the asset `pop integrate` publishes under pop's data
// directory: one document reached two ways, so the tracker a skill reads off
// disk and the tracker a convention resolves to cannot drift.
func IssueTrackerDoc() []byte {
	raw, err := docs.ReadFile("issue-tracker.md")
	if err != nil {
		panic("conventions/shipped: issue-tracker.md is not embedded: " + err.Error())
	}
	return raw
}
