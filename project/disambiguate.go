package project

import (
	"path/filepath"
	"strings"
)

// DisambiguateNames modifies the Name field of ExpandedProjects that share
// the same name by appending the first differing parent directory segment
// in parentheses. For example, if two projects both named "myapp" live at
// /work/frontend/myapp and /work/backend/myapp, they become
// "myapp (frontend)" and "myapp (backend)".
//
// When no single ancestor level disambiguates all items (e.g., 4 items
// where pairs collide at different levels), it falls back to compound
// disambiguators like "myapp (work/frontend)".
func DisambiguateNames(items []ExpandedProject) {
	// Group indices by display name
	groups := map[string][]int{}
	for i, item := range items {
		groups[item.Name] = append(groups[item.Name], i)
	}

	for _, indices := range groups {
		if len(indices) <= 1 {
			continue
		}
		disambiguateGroup(items, indices)
	}
}

func disambiguateGroup(items []ExpandedProject, indices []int) {
	type info struct {
		index    int
		segments []string // parent dir segments, innermost first
	}

	infos := make([]info, len(indices))
	maxLevels := 0
	for j, idx := range indices {
		parent := parentDir(items[idx].Path, items[idx].Name)
		segs := splitParentSegments(parent)
		infos[j] = info{index: idx, segments: segs}
		if len(segs) > maxLevels {
			maxLevels = len(segs)
		}
	}

	resolved := make(map[int]bool)

	// Phase 1: try to resolve each item with a single segment.
	// At each level, items whose segment is unique among unresolved items
	// get that single segment as their disambiguator.
	for level := 0; level < maxLevels && len(resolved) < len(infos); level++ {
		counts := map[string]int{}
		for i := range infos {
			if resolved[i] {
				continue
			}
			if level < len(infos[i].segments) {
				counts[infos[i].segments[level]]++
			}
		}
		for i := range infos {
			if resolved[i] {
				continue
			}
			if level < len(infos[i].segments) && counts[infos[i].segments[level]] == 1 {
				items[infos[i].index].Name += " (" + infos[i].segments[level] + ")"
				resolved[i] = true
			}
		}
	}

	// Phase 2: fallback for items that couldn't be resolved with a single
	// segment. Build compound disambiguators (e.g., "work/frontend")
	// progressively until all are unique.
	if len(resolved) < len(infos) {
		disambigs := make([]string, len(infos))
		for level := 0; level < maxLevels; level++ {
			allExhausted := true
			for i := range infos {
				if resolved[i] {
					continue
				}
				if level < len(infos[i].segments) {
					allExhausted = false
					seg := infos[i].segments[level]
					if disambigs[i] == "" {
						disambigs[i] = seg
					} else {
						disambigs[i] = seg + "/" + disambigs[i]
					}
				}
			}
			if allExhausted {
				break
			}

			// Check if all unresolved now have unique compound disambiguators
			counts := map[string]int{}
			for i := range infos {
				if resolved[i] {
					continue
				}
				counts[disambigs[i]]++
			}
			allUnique := true
			for i := range infos {
				if resolved[i] {
					continue
				}
				if counts[disambigs[i]] != 1 {
					allUnique = false
					break
				}
			}
			if allUnique {
				break
			}
		}

		for i := range infos {
			if !resolved[i] && disambigs[i] != "" {
				items[infos[i].index].Name += " (" + disambigs[i] + ")"
			}
		}
	}
}

// parentDir returns the parent directory of a project path, accounting for
// the number of path segments in the project name. For example, if name is
// "project/worktree" and path is "/a/b/project/worktree", parentDir returns
// "/a/b".
func parentDir(path, name string) string {
	nameSegments := len(strings.Split(name, "/"))
	parent := path
	for i := 0; i < nameSegments; i++ {
		parent = filepath.Dir(parent)
	}
	return parent
}

// splitParentSegments splits a directory path into segments from innermost
// to outermost. For "/a/b/c", it returns ["c", "b", "a"].
func splitParentSegments(dir string) []string {
	var segments []string
	for dir != "/" && dir != "." && dir != "" {
		segments = append(segments, filepath.Base(dir))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return segments
}
