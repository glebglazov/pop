package tasks

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// HandleFoldLanding resumes the attended choice for an already rebased Fold.
// The caller owns landing and rollback; an exit leaves the scratch ref intact.
func HandleFoldLanding(d *Deps, cfg *config.Config, ctx FoldConflictContext, opts FoldConflictAssistanceOptions) error {
	if d == nil {
		d = defaultDeps
	}
	in, out := opts.In, opts.Out
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return runFoldLandingGate(d, NewAttendedSession(cfg, opts.AgentPreset), ctx, opts, out, in, newPromptReader(in))
}

func runFoldLandingGate(d *Deps, gate *AttendedSession, ctx FoldConflictContext, opts FoldConflictAssistanceOptions, out io.Writer, in io.Reader, reader *promptReader) error {
	if opts.Yes || !canPrompt(in) {
		return nil
	}
	subject := ctx.SetID
	if strings.TrimSpace(subject) == "" {
		subject = ctx.RuntimePath
	}
	items := []ui.GateMenuItem{{Key: "1", Label: "Land now (default)", Default: true}}
	if strings.TrimSpace(ctx.SetID) != "" {
		items = append(items, ui.GateMenuItem{Key: "2", Label: "Verify set"})
	}
	items = append(items,
		ui.GateMenuItem{Key: "3", Label: "Abandon fold", Details: []string{"restore the branch and delete the fold scratch branch"}},
		ui.GateMenuItem{Key: "0", Label: "Exit", Details: []string{"leave the rebased fold parked for a later fold to resume"}},
	)
	for {
		preamble, err := foldLandingEvidence(d, gate.Value(), ctx)
		if err != nil {
			return err
		}
		choice, _, err := promptGateMenu(out, in, reader, ui.GateMenuSpec{
			Headline: fmt.Sprintf("Fold landing gate: %s is rebased onto trunk.", subject),
			Tone:     ui.GateMenuToneDefault, Preamble: preamble, Items: items,
		}, nil, gate)
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			return nil
		case "2":
			if err := runFoldSetVerify(d, gate.Value(), ctx, opts, out); err != nil {
				if isInterrupted(err) {
					reportTreeStableRefusal(out, "Verify set", err)
					continue
				}
				fmt.Fprintf(outputFor(out), "Verify set: %v\n", err)
			}
		case "3":
			return ErrFoldAbandon
		default:
			return fmt.Errorf("fold stopped: rebased fold scratch branch %s is parked in %s; trunk unchanged", ctx.ScratchBranch, ctx.RuntimePath)
		}
	}
}

func foldLandingEvidence(d *Deps, cfg *config.Config, ctx FoldConflictContext) ([]string, error) {
	var lines []string
	for _, tip := range []struct{ label, ref string }{
		{"Trunk", ctx.TrunkBranch},
		{"Pre-fold branch", ctx.SetBranch},
		{"Rebased scratch", ctx.ScratchBranch},
	} {
		text, err := d.Git.CommandInDir(ctx.RuntimePath, "log", "-1", "--format=%h %s (%cr)", "refs/heads/"+tip.ref, "--")
		if err != nil {
			return nil, fmt.Errorf("fold refused: read tip of %s: %w", tip.ref, err)
		}
		lines = append(lines, fmt.Sprintf("  %s: %s — %s", tip.label, tip.ref, strings.TrimSpace(text)))
	}
	count, err := d.Git.CommandInDir(ctx.RuntimePath, "rev-list", "--count", "refs/heads/"+ctx.TrunkBranch+"..refs/heads/"+ctx.ScratchBranch, "--")
	if err != nil {
		return nil, fmt.Errorf("fold refused: count commits over trunk: %w", err)
	}
	lines = append(lines, fmt.Sprintf("  Commits over trunk: %s", strings.TrimSpace(count)))
	if text := VerifiedAtBadgeText(foldConflictVerifiedBadge(d, cfg, ctx.SetID, ctx.RuntimePath)); text != "" {
		lines = append(lines, "  "+text)
	}
	return lines, nil
}
