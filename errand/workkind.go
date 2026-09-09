package errand

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

const (
	VerbRetry          work.Verb = "retry"
	VerbDismiss        work.Verb = "dismiss"
	VerbCopyOutputPath work.Verb = "copy-output-path"
)

// Kind exposes only Errand failures to Work. Queued, running, and successful
// Errands stay invisible machinery.
type Kind struct {
	tasks *tasks.Deps
}

var _ work.Kind = (*Kind)(nil)
var _ work.ArtifactSource = (*Kind)(nil)

func NewKind(td *tasks.Deps) *Kind {
	if td == nil {
		td = tasks.DefaultDeps()
	}
	return &Kind{tasks: td}
}

func (*Kind) ID() work.KindID { return ref.KindErrandFailure }

func (k *Kind) Load() ([]work.Container, error) {
	s, ok, err := k.tasks.Store(false)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.ListErrands()
	if err != nil {
		return nil, err
	}
	containers := make([]work.Container, 0, len(rows))
	for _, e := range rows {
		if e.State != store.ErrandFailed {
			continue
		}
		projectName := filepath.Base(e.WorkingPath)
		if projectName == "." || projectName == string(filepath.Separator) {
			projectName = "Errand"
		}
		containers = append(containers, work.Container{
			Kind:             ref.KindErrandFailure,
			ID:               failureID(e.Path),
			CreatedAt:        e.QueuedAt,
			Project:          projectName,
			Status:           "FAILED",
			CursorKey:        string(ref.KindErrandFailure) + "\x00" + e.Path,
			Worktree:         e.Path,
			ErrandSubject:    e.Path,
			ErrandOutputPath: e.OutputPath,
			DetailSections: []work.Section{{
				Title: "Checkout removal stopped",
				Body:  fmt.Sprintf("Checkout: %s\nOutput: %s", e.Path, e.OutputPath),
			}},
		})
	}
	return containers, nil
}

func (*Kind) Less(a, b work.Container) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID < b.ID
}

func (k *Kind) StatusCell(c work.Container) []work.StatusSegment {
	segments := []work.StatusSegment{{Text: c.Status, Tone: work.ToneLabel}}
	if c.ErrandOutputPath != "" {
		segments = append(segments, work.StatusSegment{Text: "output: " + c.ErrandOutputPath, Tone: work.ToneBad})
	}
	return segments
}

func (*Kind) Actions(work.Container) []work.Action {
	return []work.Action{
		{Verb: VerbRetry, Key: "r", Label: "retry"},
		{Verb: VerbDismiss, Key: "x", Label: "dismiss"},
	}
}

func (*Kind) StatusActions(work.Container) []work.Action { return nil }

func (*Kind) CopyActions(work.Container) []work.Action {
	return []work.Action{
		{Verb: work.VerbCopyName, Key: "n", Label: "copy name"},
		{Verb: VerbCopyOutputPath, Key: "o", Label: "copy output path"},
	}
}

func (*Kind) ItemActions(work.Container, work.Item) []work.Action { return nil }

func (k *Kind) Perform(c work.Container, _ *work.Item, verb work.Verb) (work.Outcome, error) {
	switch verb {
	case work.VerbCopyName:
		return copied(c.ID), nil
	case VerbCopyOutputPath:
		if strings.TrimSpace(c.ErrandOutputPath) == "" {
			return work.Outcome{}, fmt.Errorf("Errand failure %q has no output document", c.ID)
		}
		return copied(c.ErrandOutputPath), nil
	case VerbRetry:
		s, ok, err := k.tasks.Store(false)
		if err != nil {
			return work.Outcome{}, err
		}
		if !ok {
			return work.Outcome{}, fmt.Errorf("Errand failure %q no longer exists", c.ID)
		}
		retried, err := s.RetryErrand(c.ErrandSubject, k.tasks.Now())
		if err != nil {
			return work.Outcome{}, err
		}
		if !retried {
			return work.Outcome{}, fmt.Errorf("Errand failure %q no longer exists", c.ID)
		}
		wake(k.tasks)
		return work.Outcome{Kind: work.OutcomeRefresh, Message: "retrying Checkout removal: " + c.ErrandSubject}, nil
	case VerbDismiss:
		s, ok, err := k.tasks.Store(false)
		if err != nil {
			return work.Outcome{}, err
		}
		if !ok {
			return work.Outcome{}, fmt.Errorf("Errand failure %q no longer exists", c.ID)
		}
		dismissed, err := s.DismissErrand(c.ErrandSubject)
		if err != nil {
			return work.Outcome{}, err
		}
		if !dismissed {
			return work.Outcome{}, fmt.Errorf("Errand failure %q no longer exists", c.ID)
		}
		return work.Outcome{Kind: work.OutcomeRefresh, Message: "dismissed Errand failure: " + c.ErrandSubject}, nil
	default:
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
}

func (*Kind) Summary(containers []work.Container) []string {
	if len(containers) == 0 {
		return nil
	}
	return []string{work.CountPhrase(len(containers), "Errand failure", "Errand failures")}
}

func (*Kind) Columns() []string {
	return []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}
}

func (*Kind) TypeWords() []string { return []string{"errand", "failure", "checkout removal"} }

func (k *Kind) Artifacts(c work.Container) ([]work.Artifact, error) {
	if c.ErrandOutputPath == "" {
		return nil, nil
	}
	return []work.Artifact{{
		Type: "output",
		Name: filepath.Base(c.ErrandOutputPath),
		Path: c.ErrandOutputPath,
		At:   c.CreatedAt,
	}}, nil
}

func (*Kind) ArtifactActions(work.Container, work.Artifact) []work.Action {
	return []work.Action{
		{Verb: work.VerbCopyName, Key: "y", Label: "copy name"},
		{Verb: VerbCopyOutputPath, Key: "p", Label: "copy path"},
	}
}

func (k *Kind) PerformArtifact(_ work.Container, artifact work.Artifact, verb work.Verb) (work.Outcome, error) {
	switch verb {
	case work.VerbCopyName:
		return copied(artifact.Name), nil
	case VerbCopyOutputPath:
		return copied(artifact.Path), nil
	default:
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
}

func copied(payload string) work.Outcome {
	return work.Outcome{Kind: work.OutcomeMessage, Clipboard: payload, Message: "copied " + payload}
}

func failureID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("remove checkout %s [%x]", filepath.Base(path), sum[:3])
}
