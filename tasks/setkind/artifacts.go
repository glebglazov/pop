package setkind

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

const (
	artifactTypeReview   = tasks.ArtifactTypeReview
	artifactTypeSpec     = tasks.ArtifactTypeSpec
	artifactTypeProgress = tasks.ArtifactTypeProgress
	progressFileName     = tasks.ProgressFileName
	reviewsDirName       = tasks.ReviewsDirName
)

var _ work.ArtifactSource = (*Kind)(nil)

// Artifacts publishes the readable documents held by one Task set. The root
// listing supplies both membership and modification times for the two files
// that do not date themselves; reviews use the instant already in each name.
func (k *Kind) Artifacts(c work.Container) ([]work.Artifact, error) {
	setDir := filepath.Join(c.DefPath, c.ID)
	listed, err := tasks.Artifacts(k.d.Tasks, setDir)
	if err != nil {
		return nil, fmt.Errorf("read artifacts for task set %q: %w", c.ID, err)
	}
	artifacts := make([]work.Artifact, 0, len(listed))
	for _, artifact := range listed {
		artifacts = append(artifacts, work.Artifact{
			Type: artifact.Type,
			Name: artifact.Name,
			Path: artifact.Path,
			At:   artifact.At,
		})
	}
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
