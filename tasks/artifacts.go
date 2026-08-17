package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	ArtifactTypeReview   = "review"
	ArtifactTypeSpec     = "spec"
	ArtifactTypeProgress = "progress"
	ProgressFileName     = "progress.txt"
	ReviewsDirName       = "reviews"
)

// ArtifactSectionTitle is shared by the CLI and dashboard detail surfaces so
// both name the same summary block.
const ArtifactSectionTitle = "Artifacts"

// Artifact is one readable document published by a Task set.
type Artifact struct {
	Type string
	Name string
	Path string
	At   time.Time
}

// Artifacts returns the closed list of Task-set artifacts, newest first.
func Artifacts(d *Deps, setDir string) ([]Artifact, error) {
	if d == nil {
		d = defaultDeps
	}
	entries, err := d.FS.ReadDir(setDir)
	if err != nil {
		return nil, err
	}

	artifacts := make([]Artifact, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var artifactType string
		switch entry.Name() {
		case SpecFileName:
			artifactType = ArtifactTypeSpec
		case ProgressFileName:
			artifactType = ArtifactTypeProgress
		default:
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read artifact %s: %w", entry.Name(), err)
		}
		artifacts = append(artifacts, Artifact{
			Type: artifactType,
			Name: entry.Name(),
			Path: filepath.Join(setDir, entry.Name()),
			At:   info.ModTime(),
		})
	}

	reviewDir := filepath.Join(setDir, ReviewsDirName)
	reviews, err := d.FS.ReadDir(reviewDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range reviews {
		at, ok := ReviewFileInstant(entry.Name())
		if entry.IsDir() || !ok {
			continue
		}
		artifacts = append(artifacts, Artifact{
			Type: ArtifactTypeReview,
			Name: entry.Name(),
			Path: filepath.Join(reviewDir, entry.Name()),
			At:   at,
		})
	}

	sort.Slice(artifacts, func(i, j int) bool {
		if !artifacts[i].At.Equal(artifacts[j].At) {
			return artifacts[i].At.After(artifacts[j].At)
		}
		if artifacts[i].Name != artifacts[j].Name {
			return artifacts[i].Name < artifacts[j].Name
		}
		return artifacts[i].Type < artifacts[j].Type
	})
	return artifacts, nil
}

// ArtifactSummary gives detail surfaces enough information to show that the
// Artifact view is useful without repeating a document path.
type ArtifactSummary struct {
	Count      int
	NewestType string
	NewestAt   time.Time
}

// LatestArtifactSummary derives one summary from the same ordered list used by
// the Artifact view. An unreadable or empty list has no summary block.
func LatestArtifactSummary(d *Deps, setDir string) (ArtifactSummary, bool) {
	artifacts, err := Artifacts(d, setDir)
	if err != nil || len(artifacts) == 0 {
		return ArtifactSummary{}, false
	}
	return ArtifactSummary{
		Count:      len(artifacts),
		NewestType: artifacts[0].Type,
		NewestAt:   artifacts[0].At,
	}, true
}

// Body renders the facts shared by the CLI and dashboard detail surfaces.
func (s ArtifactSummary) Body() string {
	noun := "artifacts"
	if s.Count == 1 {
		noun = "artifact"
	}
	return fmt.Sprintf("%d %s\nnewest: %s, written %s",
		s.Count, noun, s.NewestType, s.NewestAt.UTC().Format("2006-01-02 15:04Z"))
}
