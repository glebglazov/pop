package routine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manifest is the on-disk record for a Routine.
type Manifest struct {
	BoundDirectory string `json:"bound_directory"`
	Schedule       string `json:"schedule"`
	Paused         bool   `json:"paused"`
	CreatedAt      string `json:"created_at"`
}

// Routine is a discovered Routine with its identifier and parsed manifest.
type Routine struct {
	ID       string
	Manifest Manifest
	Schedule Schedule
}

func loadManifest(d *Deps, id string) (*Routine, error) {
	dir := routineDir(d, id)
	path := filepath.Join(dir, manifestFileName)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("routine %q not found", id)
		}
		return nil, fmt.Errorf("read routine manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse routine manifest %q: %w", id, err)
	}
	sched, err := ParseSchedule(m.Schedule)
	if err != nil {
		return nil, fmt.Errorf("routine %q has invalid schedule: %w", id, err)
	}
	return &Routine{ID: id, Manifest: m, Schedule: sched}, nil
}

func writeManifest(d *Deps, id string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode routine manifest: %w", err)
	}
	path := filepath.Join(routineDir(d, id), manifestFileName)
	if err := d.FS.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write routine manifest: %w", err)
	}
	return nil
}

func validateID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("routine id is required")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("invalid routine id %q", id)
	}
	if strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid routine id %q: must not contain path separators", id)
	}
	return nil
}

func canonicalBoundDirectory(d *Deps, cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = d.FS.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine working directory: %w", err)
		}
	}
	expanded := expandHome(d, cwd)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve bound directory: %w", err)
	}
	clean := filepath.Clean(abs)
	resolved, err := d.FS.EvalSymlinks(clean)
	if err != nil {
		resolved = clean
	}
	return resolved, nil
}

func expandHome(d *Deps, path string) string {
	if path == "~" {
		home, err := d.FS.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := d.FS.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func nowUTC(d *Deps) time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}
