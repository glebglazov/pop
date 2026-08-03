package queuetest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// DataDirSnapshot hashes every file under the pop data dir. The sqlite shared-
// memory sidecar is excluded: it is mmap coordination state a plain read may
// touch, while a real write always lands in the database or its write-ahead log.
func DataDirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(path, "-shm") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(dir, path)
		snapshot[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return snapshot
}

func SameSnapshot(before, after map[string]string) bool {
	return snapshotDiff(before, after) == ""
}

func AssertSameSnapshot(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if diff := snapshotDiff(before, after); diff != "" {
		t.Fatalf("%s wrote to the data dir:\n%s", what, diff)
	}
}

func snapshotDiff(before, after map[string]string) string {
	var diffs []string
	for name, sum := range after {
		switch prev, ok := before[name]; {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("  created %s", name))
		case prev != sum:
			diffs = append(diffs, fmt.Sprintf("  changed %s", name))
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			diffs = append(diffs, fmt.Sprintf("  removed %s", name))
		}
	}
	sort.Strings(diffs)
	return strings.Join(diffs, "\n")
}
