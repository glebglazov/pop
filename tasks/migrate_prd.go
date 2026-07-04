package tasks

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// legacyPRDSubdir is the retired sibling directory that held PRDs before they
// co-located into their Task set's folder (ADR-0088), relative to the storage
// directory (the parent of tasks/).
const legacyPRDSubdir = "prds"

// coLocatedPRDName is the filename a PRD takes once it lives inside its Task
// set's folder.
const coLocatedPRDName = "prd.md"

// timestampPrefix matches the human-readable date/time prefix that both PRD
// filenames and Task-set folders carry — YYYY-MM-DD optionally followed by a
// -HHMM time — so the trailing slug can be isolated for matching.
var timestampPrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(-\d{4})?-(.+)$`)

// PRDColocationMigration summarizes a one-shot move of sibling prds/<slug>.md
// files into their matching Task set's folder as prd.md.
type PRDColocationMigration struct {
	// Moved lists the "<prd-file> -> <set>" relocations performed, sorted.
	Moved []string
	// Unmatched lists retired prds/ filenames left in place because no single
	// destination set matched, sorted.
	Unmatched []string
}

// prdSlug strips the leading timestamp prefix from a PRD filename stem or a
// Task-set folder name, yielding the shared slug used to match the two. A name
// without a recognizable timestamp prefix is its own slug.
func prdSlug(name string) string {
	if m := timestampPrefix.FindStringSubmatch(name); m != nil {
		return m[2]
	}
	return name
}

// MigratePRDColocation moves each sibling prds/<slug>.md into its matching Task
// set folder as tasks/<set>/prd.md, matched by slug (the filename stem and the
// set folder name compared with their timestamp prefixes stripped). A PRD is
// moved only when exactly one set matches by slug and that set has no prd.md
// yet; an unmatched, ambiguous, or already-occupied PRD is left untouched and
// reported. It is idempotent — a moved PRD leaves nothing behind to move again —
// and never overwrites an existing prd.md. A missing prds/ directory is a clean
// no-op; a nil summary means nothing was migrated.
func MigratePRDColocation(d *Deps, tasksDir string) (*PRDColocationMigration, error) {
	prdsDir := filepath.Join(filepath.Dir(tasksDir), legacyPRDSubdir)

	entries, err := d.FS.ReadDir(prdsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, exitErr(ExitOperational, "inspect prds directory %s: %v", prdsDir, err)
	}

	// Index Task-set folders by slug so each PRD can find its destination. A slug
	// shared by more than one set makes the destination ambiguous.
	setsBySlug := map[string][]string{}
	for _, setID := range readTaskSetIDs(d, tasksDir) {
		slug := prdSlug(setID)
		setsBySlug[slug] = append(setsBySlug[slug], setID)
	}

	result := &PRDColocationMigration{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		matches := setsBySlug[prdSlug(stem)]
		if len(matches) != 1 {
			result.Unmatched = append(result.Unmatched, e.Name())
			continue
		}

		dst := filepath.Join(tasksDir, matches[0], coLocatedPRDName)
		if _, statErr := d.FS.Stat(dst); statErr == nil {
			// A co-located PRD already exists — never overwrite it.
			result.Unmatched = append(result.Unmatched, e.Name())
			continue
		} else if !os.IsNotExist(statErr) {
			return nil, exitErr(ExitOperational, "inspect co-located PRD %s: %v", dst, statErr)
		}

		src := filepath.Join(prdsDir, e.Name())
		if err := moveTree(d, src, dst); err != nil {
			return nil, exitErr(ExitOperational, "move PRD %q into set %q: %v", e.Name(), matches[0], err)
		}
		result.Moved = append(result.Moved, e.Name()+" -> "+matches[0])
	}

	sort.Strings(result.Moved)
	sort.Strings(result.Unmatched)

	if len(result.Moved) == 0 && len(result.Unmatched) == 0 {
		return nil, nil
	}

	if len(result.Moved) > 0 {
		noticeOut := outputFor(noticeWriter(d))
		noticeOut.line(ansiCyan, "Co-located %d PRD(s) into their task sets", len(result.Moved))
	}

	return result, nil
}
