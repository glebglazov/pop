package routine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const promptStub = `# Routine prompt

Describe what this routine should do on each run.
`

// AddResult is the outcome of scaffolding a new Routine.
type AddResult struct {
	ID       string
	Dir      string
	Manifest Manifest
}

// Add scaffolds a new Routine using default dependencies.
func Add(id, scheduleRaw, cwd string) (*AddResult, error) {
	return AddWith(defaultDeps, id, scheduleRaw, cwd)
}

// AddWith scaffolds routines/<id>/ under pop's data dir.
func AddWith(d *Deps, id, scheduleRaw, cwd string) (*AddResult, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	if _, err := ParseSchedule(scheduleRaw); err != nil {
		return nil, err
	}

	boundDir, err := canonicalBoundDirectory(d, cwd)
	if err != nil {
		return nil, err
	}

	dir := routineDir(d, id)
	if _, err := d.FS.Stat(dir); err == nil {
		return nil, fmt.Errorf("routine %q already exists at %s", id, dir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect routine directory: %w", err)
	}

	if err := d.FS.MkdirAll(filepath.Join(dir, memoryDirName), 0o755); err != nil {
		return nil, fmt.Errorf("create routine memory directory: %w", err)
	}
	if err := d.FS.MkdirAll(filepath.Join(dir, runsDirName), 0o755); err != nil {
		return nil, fmt.Errorf("create routine runs directory: %w", err)
	}

	promptPath := filepath.Join(dir, promptFileName)
	if err := d.FS.WriteFile(promptPath, []byte(promptStub), 0o644); err != nil {
		return nil, fmt.Errorf("write routine prompt: %w", err)
	}

	manifest := Manifest{
		BoundDirectory: boundDir,
		Schedule:       strings.TrimSpace(scheduleRaw),
		Paused:         true,
		CreatedAt:      nowUTC(d).Format(timeRFC3339),
	}
	if err := writeManifest(d, id, manifest); err != nil {
		return nil, err
	}

	if d.IsInteractive != nil && d.IsInteractive() && d.OpenEditor != nil {
		if err := d.OpenEditor(promptPath); err != nil {
			return nil, fmt.Errorf("open prompt in editor: %w", err)
		}
	}

	return &AddResult{ID: id, Dir: dir, Manifest: manifest}, nil
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"
