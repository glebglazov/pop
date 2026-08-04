package wayfinder

import (
	"regexp"
	"strings"
)

// map.md has two writers. The prose sections belong to the grilling session; the
// index sections belong to pop, which rebuilds them from the manifest on every
// resolve. Under parallel grilling panes that split is what makes concurrent
// appends safe — a generated region is never merged, only replaced — so the
// markers exist to tell a session exactly which lines it must not touch.
const (
	generatedRegionOpen  = "<!-- pop:generated %s -->"
	generatedRegionClose = "<!-- /pop:generated %s -->"
)

var (
	outOfScopeHeader  = regexp.MustCompile(`(?i)^##\s+Out of scope\s*$`)
	spawnedSetsHeader = regexp.MustCompile(`(?i)^##\s+Spawned sets\s*$`)
)

// generatedSection is one pop-owned region of map.md: the heading it lives
// under, the marker name that delimits it, and the lines to put between the
// markers.
type generatedSection struct {
	name    string
	heading string
	header  *regexp.Regexp
	body    []string
}

func generatedRegionMarkers(name string) (string, string) {
	return strings.Replace(generatedRegionOpen, "%s", name, 1),
		strings.Replace(generatedRegionClose, "%s", name, 1)
}

// locateGeneratedRegion finds a region's marker pair anywhere in the file. The
// scan is deliberately unbounded by section: a generated body may carry its own
// `## ` headings, and a heading-bounded scan would stop before the close marker,
// mistake the rest of the region for someone else's content, and leave it behind
// on the next write.
func locateGeneratedRegion(lines []string, name string) (open, close int, found bool) {
	openMarker, closeMarker := generatedRegionMarkers(name)
	open, close = -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case openMarker:
			if open < 0 {
				open = i
			}
		case closeMarker:
			if open >= 0 && close < 0 {
				close = i
			}
		}
	}
	return open, close, open >= 0 && close > open
}

// generatedRegionBody returns what a region currently holds, blank lines and all,
// or reports that the region is absent.
func generatedRegionBody(lines []string, name string) (string, bool) {
	open, close, found := locateGeneratedRegion(lines, name)
	if !found {
		return "", false
	}
	return strings.Join(lines[open+1:close], "\n"), true
}

// renderGeneratedSections rewrites every pop-owned region of map.md and leaves
// the rest of the file byte-for-byte alone.
func renderGeneratedSections(content string, sections []generatedSection) string {
	lines := strings.Split(content, "\n")
	for _, section := range sections {
		lines = applyGeneratedSection(lines, section)
	}
	return strings.Join(trimTrailingBlank(lines), "\n") + "\n"
}

// applyGeneratedSection replaces one region. Between existing markers only the
// marked lines change, so prose a session wrote above or below them inside the
// same section survives. A section with no markers yet is taken over whole: the
// convention is new, and merging hand-written index lines with generated ones
// would duplicate every decision already recorded there.
func applyGeneratedSection(lines []string, section generatedSection) []string {
	block := generatedRegionBlock(section.name, section.body)
	if openIdx, closeIdx, marked := locateGeneratedRegion(lines, section.name); marked {
		out := append([]string{}, lines[:openIdx]...)
		out = append(out, block...)
		return append(out, lines[closeIdx+1:]...)
	}

	start, end, found := sectionBounds(lines, section.header)
	if !found {
		out := append([]string{}, trimTrailingBlank(lines)...)
		out = append(out, "", "## "+section.heading, "")
		return append(out, block...)
	}

	out := append([]string{}, lines[:start]...)
	out = append(out, "")
	out = append(out, block...)
	out = append(out, "")
	return append(out, lines[end:]...)
}

func generatedRegionBlock(name string, body []string) []string {
	openMarker, closeMarker := generatedRegionMarkers(name)
	if len(body) == 0 {
		return []string{openMarker, closeMarker}
	}
	block := []string{openMarker, ""}
	block = append(block, body...)
	return append(block, "", closeMarker)
}
