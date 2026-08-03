package wayfinder

import (
	"regexp"
	"strings"
)

// map.md has two writers. The prose sections belong to the grilling session; the
// index sections belong to pop, which rebuilds them from the manifest on every
// resolve. Under parallel grilling windows that split is what makes concurrent
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
	block := generatedBlock(section)
	start, end, found := sectionBounds(lines, section.header)
	if !found {
		out := append([]string{}, trimTrailingBlank(lines)...)
		out = append(out, "", "## "+section.heading, "")
		return append(out, block...)
	}

	openMarker, closeMarker := generatedRegionMarkers(section.name)
	openIdx, closeIdx := -1, -1
	for i := start; i < end; i++ {
		switch strings.TrimSpace(lines[i]) {
		case openMarker:
			if openIdx < 0 {
				openIdx = i
			}
		case closeMarker:
			closeIdx = i
		}
	}

	out := append([]string{}, lines[:start]...)
	if openIdx >= 0 && closeIdx > openIdx {
		out = append(out, lines[start:openIdx]...)
		out = append(out, block...)
		out = append(out, lines[closeIdx+1:end]...)
	} else {
		out = append(out, "")
		out = append(out, block...)
		out = append(out, "")
	}
	return append(out, lines[end:]...)
}

func generatedBlock(section generatedSection) []string {
	openMarker, closeMarker := generatedRegionMarkers(section.name)
	if len(section.body) == 0 {
		return []string{openMarker, closeMarker}
	}
	block := []string{openMarker, ""}
	block = append(block, section.body...)
	return append(block, "", closeMarker)
}
