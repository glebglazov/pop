package wayfinder

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// FindMap resolves a bare map identifier to one scanned Map.
func FindMap(d *Deps, cwd, raw string) (Map, error) {
	resolved, err := resolveMapTarget(raw)
	if err != nil {
		return Map{}, err
	}
	maps, err := ScanMaps(d, cwd)
	if err != nil {
		return Map{}, err
	}
	for _, m := range maps {
		if m.ID == resolved {
			return m, nil
		}
	}
	return Map{}, fmt.Errorf("unknown wayfinder map %q; valid: %s", resolved, mapIDList(maps))
}

func resolveMapTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("expected a bare map identifier")
	}
	if filepath.IsAbs(raw) || raw == "~" || strings.HasPrefix(filepath.ToSlash(raw), "~/") {
		return "", fmt.Errorf("invalid target %q: absolute paths are not map references", raw)
	}
	if raw == "." || raw == ".." || strings.HasPrefix(filepath.ToSlash(raw), "./") || strings.HasPrefix(filepath.ToSlash(raw), "../") {
		return "", fmt.Errorf("invalid target %q: relative paths are not map references", raw)
	}
	slash := strings.TrimSuffix(filepath.ToSlash(raw), "/")
	if strings.Contains(slash, "/") || strings.HasSuffix(slash, ".md") {
		return "", fmt.Errorf("invalid target %q: expected a bare map identifier", raw)
	}
	return slash, nil
}

func mapIDList(maps []Map) string {
	if len(maps) == 0 {
		return "(none)"
	}
	ids := make([]string, len(maps))
	for i, m := range maps {
		ids[i] = m.ID
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}
