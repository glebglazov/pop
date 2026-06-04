package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gitignoreLine is the single ignore entry pop's workload-gitignore step
// manages: the workload artifacts directory. Adding it to the user's global git
// ignore keeps workload planning output (thoughts/) machine-local in every
// repository without a per-repo .gitignore edit.
const gitignoreLine = "thoughts/"

// resolveGitignoreTarget resolves the file the gitignore step appends to,
// WITHOUT ever mutating git config (ADR 0010: pop never runs config-changing
// git commands on the user's behalf).
//
//   - If core.excludesfile is set, that configured file is the target (with a
//     leading ~ expanded). pop's line is appended there, respecting the user's
//     existing setup.
//   - Otherwise git's default global ignore path is used:
//     $XDG_CONFIG_HOME/git/ignore when XDG_CONFIG_HOME is set, else
//     ~/.config/git/ignore. Git reads this path by default with no config entry,
//     so using it leaves git config untouched.
func resolveGitignoreTarget(d *integrateDeps) (string, error) {
	home, err := d.userHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Read-only probe of core.excludesfile. A missing key (non-zero git exit)
	// or an empty value falls through to git's default global ignore path.
	if d.gitConfig != nil {
		if v, err := d.gitConfig("core.excludesfile"); err == nil {
			if v = strings.TrimSpace(v); v != "" {
				return expandTilde(v, home), nil
			}
		}
	}

	if xdg := d.getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git", "ignore"), nil
	}
	return filepath.Join(home, ".config", "git", "ignore"), nil
}

// expandTilde expands a leading ~ in p to the user's home directory. Git stores
// core.excludesfile values like "~/.gitignore_global" verbatim, so the path
// must be expanded before use.
func expandTilde(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// gitignoreConfigured reports whether the workload gitignore line is already
// present in the resolved target file, along with the resolved target path. A
// missing target file reports not-configured (the resolved path is still
// returned so callers can name it). Used by the wizard for idempotency and by
// Doctor for state reporting.
func gitignoreConfigured(d *integrateDeps) (configured bool, target string, err error) {
	target, err = resolveGitignoreTarget(d)
	if err != nil {
		return false, "", err
	}
	data, err := d.readFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, target, nil
		}
		return false, target, fmt.Errorf("failed to read %s: %w", target, err)
	}
	return hasGitignoreLine(data), target, nil
}

// hasGitignoreLine reports whether the workload ignore line appears as a
// standalone line in the file contents (ignoring surrounding whitespace).
func hasGitignoreLine(data []byte) bool {
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) == gitignoreLine {
			return true
		}
	}
	return false
}

// installGitignore appends the workload gitignore line to the resolved target
// file, idempotently. If the line is already present, nothing is written and an
// already-configured note is printed. Git config is never modified.
//
// The effect is global and agent-independent; the agent argument is accepted to
// satisfy the catalog install signature but only anchors the command form
// (`pop integrate <agent> --workload-gitignore`).
func installGitignore(d *integrateDeps, _ string, _ string) error {
	configured, target, err := gitignoreConfigured(d)
	if err != nil {
		return err
	}
	if configured {
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "%s already configured in %s\n", gitignoreLine, target)
		}
		return nil
	}

	existing, err := d.readFile(target)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", target, err)
	}

	buf := append([]byte{}, existing...)
	if len(buf) > 0 && !bytes.HasSuffix(buf, []byte("\n")) {
		buf = append(buf, '\n')
	}
	buf = append(buf, []byte(gitignoreLine+"\n")...)

	if err := d.mkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(target), err)
	}
	if err := d.writeFile(target, buf, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", target, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Added %s to %s\n", gitignoreLine, target)
	}
	return nil
}

// reportGitignoreRemoval prints the gitignore line and the file it lives in for
// manual deletion. Removal is report-only by design (ADR 0010): pop never edits
// a shared global config file out from under the user. No filesystem change is
// made.
func reportGitignoreRemoval(d *integrateDeps) error {
	configured, target, err := gitignoreConfigured(d)
	if err != nil {
		return err
	}
	if d.stdout == nil {
		return nil
	}
	if !configured {
		fmt.Fprintf(d.stdout, "%s is not configured in %s — nothing to remove\n", gitignoreLine, target)
		return nil
	}
	fmt.Fprintf(d.stdout, "To remove the workload gitignore entry, delete this line from %s manually:\n  %s\n", target, gitignoreLine)
	return nil
}
