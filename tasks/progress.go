package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AppendProgress appends a terminal progress record to progress.txt.
func AppendProgress(d *Deps, manifestDir, taskFile, outcome, summary string) error {
	path := filepath.Join(manifestDir, "progress.txt")
	block := fmt.Sprintf("%s [%s] %s\n%s\n---\n",
		time.Now().UTC().Format(time.RFC3339),
		taskFile,
		outcome,
		strings.TrimSpace(summary),
	)

	var existing []byte
	data, err := d.FS.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		existing = data
		if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
			existing = append(existing, '\n')
		}
	}
	combined := append(existing, []byte(block)...)
	return WriteAtomicWith(d, path, combined, 0o644)
}

// AppendSetProgress appends a set-level progress record to progress.txt. It is
// used for events that belong to the task set as a whole rather than to a
// single task file.
func AppendSetProgress(d *Deps, manifestDir, outcome, summary string) error {
	return AppendProgress(d, manifestDir, "set", outcome, summary)
}
