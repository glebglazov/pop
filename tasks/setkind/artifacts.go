package setkind

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

const (
	artifactTypeReview   = "review"
	artifactTypeSpec     = "spec"
	artifactTypeProgress = "progress"
	progressFileName     = "progress.txt"
	reviewsDirName       = "reviews"
)

var _ work.ArtifactSource = (*Kind)(nil)

// Artifacts publishes the readable documents held by one Task set. The root
// listing supplies both membership and modification times for the two files
// that do not date themselves; reviews use the instant already in each name.
func (k *Kind) Artifacts(c work.Container) ([]work.Artifact, error) {
	setDir := filepath.Join(c.DefPath, c.ID)
	entries, err := k.d.Tasks.FS.ReadDir(setDir)
	if err != nil {
		return nil, fmt.Errorf("read artifacts for task set %q: %w", c.ID, err)
	}

	artifacts := make([]work.Artifact, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var artifactType string
		switch entry.Name() {
		case tasks.SpecFileName:
			artifactType = artifactTypeSpec
		case progressFileName:
			artifactType = artifactTypeProgress
		default:
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read artifact %s: %w", entry.Name(), err)
		}
		artifacts = append(artifacts, work.Artifact{
			Type: artifactType,
			Name: entry.Name(),
			Path: filepath.Join(setDir, entry.Name()),
			At:   info.ModTime(),
		})
	}

	reviewDir := filepath.Join(setDir, reviewsDirName)
	reviews, err := k.d.Tasks.FS.ReadDir(reviewDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read review artifacts for task set %q: %w", c.ID, err)
	}
	for _, entry := range reviews {
		at, ok := tasks.ReviewFileInstant(entry.Name())
		if entry.IsDir() || !ok {
			continue
		}
		artifacts = append(artifacts, work.Artifact{
			Type: artifactTypeReview,
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

// ArtifactActions offers the two in-place transfers shared by every Task-set
// artifact: its visible filename and its absolute path.
func (k *Kind) ArtifactActions(work.Container, work.Artifact) []work.Action {
	return []work.Action{
		{Verb: work.VerbCopyName, Key: "y", Label: "copy name"},
		{Verb: VerbCopyPath, Key: "p", Label: "copy path"},
	}
}

// PerformArtifact performs one artifact transfer without applying Task item
// status semantics to a document.
func (k *Kind) PerformArtifact(c work.Container, artifact work.Artifact, verb work.Verb) (work.Outcome, error) {
	var payload string
	switch verb {
	case work.VerbCopyName:
		payload = artifact.Name
		setDir := filepath.Join(c.DefPath, c.ID)
		if c.DefPath != "" && c.ID != "" {
			if rel, err := filepath.Rel(setDir, artifact.Path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				payload = filepath.ToSlash(rel)
			}
		}
	case VerbCopyPath:
		payload = artifact.Path
	default:
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
	return work.Outcome{Kind: work.OutcomeMessage, Clipboard: payload, Message: "copied " + payload}, nil
}
