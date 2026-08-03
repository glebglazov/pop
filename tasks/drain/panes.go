package drain

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// runningTaggedPane returns the pane id when a tagged pane exists and its
// foreground command is not a bare shell — the green / jump case. An idle
// tagged pane (grey / respawn) returns "" so the caller falls through to
// EnsureTaggedPane. When the command cannot be read, the pane is treated as
// running so we never SendKeys into an unknown process.
func runningTaggedPane(t tmuxmod.Tmux, session string, tag tmuxmod.PaneTag, setID string) (string, error) {
	if t == nil || session == "" || setID == "" {
		return "", nil
	}
	paneID, err := t.FindTaggedPane(session, tag, setID)
	if err != nil || paneID == "" {
		return paneID, err
	}
	info, err := t.PaneInfo(paneID)
	if err != nil {
		return paneID, nil
	}
	if tmuxmod.IsBareShell(info.Command) {
		return "", nil
	}
	return paneID, nil
}

type BindEntry struct {
	Label   string
	Path    string
	Branch  string
	Create  bool
	Managed bool
}

type DrainEntry struct {
	Label  string
	Kind   DrainTargetKind
	Path   string // adopt target checkout (DrainTargetWorktree only)
	Branch string
}

func parseDashboardWorktrees(output string) []project.Worktree {
	var worktrees []project.Worktree
	var current project.Worktree
	isBare := false
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
			current.Name = filepath.Base(current.Path)
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.Branch = "detached"
		case line == "bare":
			isBare = true
		case line == "":
			if current.Path != "" && !isBare {
				worktrees = append(worktrees, current)
			}
			current = project.Worktree{}
			isBare = false
		}
	}
	if current.Path != "" && !isBare {
		worktrees = append(worktrees, current)
	}
	return worktrees
}

// DrainTargetKind identifies one Drain target picker option (ADR-0052).
type DrainTargetKind int

const (
	// DrainTargetWorktree adopts an existing non-managed, unbound worktree.
	DrainTargetWorktree DrainTargetKind = iota
	// DrainTargetNewManaged provisions a managed worktree forked from the trunk.
	DrainTargetNewManaged
	// DrainTargetTrunk drains inline in the trunk worktree with no binding.
	DrainTargetTrunk
)

// boundCheckoutPaths returns the canonicalized set of every checkout currently
// bound to a set, across all repos. The Drain target picker excludes these from
// its adopt list so a checkout never binds to two sets at once.
func boundCheckoutPaths(d *Deps) (map[string]bool, error) {
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, b := range bindings {
		path := strings.TrimSpace(b.RuntimePath)
		if path == "" {
			continue
		}
		out[bestEffortCanon(d, path)] = true
	}
	return out, nil
}

// bestEffortCanon canonicalizes path for reliable comparison, falling back to a
// cleaned absolute path when the target does not exist (so EvalSymlinks fails).
func bestEffortCanon(d *Deps, path string) string {
	if c, err := canonicalCheckoutPath(d.Tasks, path); err == nil {
		return c
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// PathUnder reports whether path is root or lives beneath it. Both arguments are
// expected to be canonicalized.
func PathUnder(path, root string) bool {
	if root == "" {
		return false
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// pathUnderAny reports whether path lies under any of roots. It serves the
// callers that must ask about a set of roots at once — the managed-worktree root
// is two directories while a machine still has worktrees waiting for the gated
// root move.
func pathUnderAny(path string, roots []string) bool {
	for _, root := range roots {
		if PathUnder(path, root) {
			return true
		}
	}
	return false
}

func parseDashboardBaseRefs(output string) []string {
	seen := map[string]bool{}
	var refs []string
	for _, line := range strings.Split(output, "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, "/HEAD") || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		ri, rj := dashboardBaseRefRank(refs[i]), dashboardBaseRefRank(refs[j])
		if ri != rj {
			return ri < rj
		}
		return refs[i] < refs[j]
	})
	return refs
}

func dashboardBaseRefRank(ref string) int {
	switch ref {
	case "main":
		return 0
	case "master":
		return 1
	}
	if strings.HasSuffix(ref, "/main") {
		return 2
	}
	if strings.HasSuffix(ref, "/master") {
		return 3
	}
	return 4
}

// storageRepoRoot derives a repository's working-tree root from the canonical
// git common directory recorded in its marker: a normal repo's common dir is
// `<root>/.git` and a bare-with-worktrees layout's is `<root>/.bare`, so the
// root is the parent; a top-level bare repo's common dir is the repo dir itself.
func storageRepoRoot(d *tasks.Deps, commonDir string) string {
	root := commonDir
	switch filepath.Base(commonDir) {
	case ".git", ".bare":
		root = filepath.Dir(commonDir)
	}
	if canon, err := canonicalCheckoutPath(d, root); err == nil {
		return canon
	}
	return root
}

// pathWithinOrEqual reports whether p is base or a descendant of base.
func pathWithinOrEqual(p, base string) bool {
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}

// dashboardRepresentative resolves a repo group's integration target without
// forking git (ADR-0060): a per-checkout `trunk = true` override wins (bare or
// not), else a non-bare repo's target is the main worktree — the parent of the
// common directory — and a bare repo with no declared trunk has none (bare=true,
// rep=nil). A renamed execution key surfaces as a fatal config finding, matching
// resolveRepresentative's contract.
func dashboardRepresentative(d *Deps, cfg *config.Config, commonDir string, scans []projectScan) (*projectScan, bool, error) {
	if cfg != nil && len(scans) > 0 {
		if _, err := resolveRepoConfigFor(d, cfg, scans[0].ProjectPath); err != nil {
			var f config.Finding
			if errors.As(err, &f) {
				return nil, false, err
			}
		}
	}

	// 1. explicit trunk = true checkout (config-only, no git).
	for i := range scans {
		rc, err := resolveRepoConfigFor(d, cfg, scans[i].ProjectPath)
		if err == nil && rc.Trunk {
			return &scans[i], false, nil
		}
	}

	// 2. non-bare repo → main worktree = parent of the common directory. A normal
	// repo's common dir is `<root>/.git`; only that layout has a derivable main
	// worktree fork-free. Anything else (`.bare`, top-level bare) is bare.
	if filepath.Base(commonDir) == ".git" {
		return dashboardScanForCheckout(d, scans, filepath.Dir(commonDir)), false, nil
	}

	// 3. bare repo with no declared trunk → no integration target.
	return nil, true, nil
}

// dashboardScanForCheckout returns the scan whose checkout canonicalizes to
// checkoutPath, or synthesizes one (fork-free) when the target — e.g. a main
// worktree that is not itself a picker Project — is not among the group's scans.
func dashboardScanForCheckout(d *Deps, scans []projectScan, checkoutPath string) *projectScan {
	canon, err := canonicalCheckoutPath(d.Tasks, checkoutPath)
	if err != nil {
		canon = checkoutPath
	}
	for i := range scans {
		if c, err := canonicalCheckoutPath(d.Tasks, scans[i].ProjectPath); err == nil && c == canon {
			return &scans[i]
		}
	}
	name, label := "", ""
	if len(scans) > 0 {
		name = scans[0].Name
		label = scans[0].ProjectLabel
	}
	// SessionName is left unset for the same reason as in dashboardRepoStatics:
	// deriving it forks git and the build path never reads it.
	return &projectScan{
		Name:         name,
		ProjectLabel: label,
		ProjectPath:  canon,
		RuntimePath:  canon,
	}
}

// WorkDataDir returns the data directory for the Work supervisor's own durable
// runtime files — its single-instance lock and its narration log. The append-only
// journal that once lived beside them is retired: the journal is now a view over
// the store (ADR-0055).
func WorkDataDir(d *tasks.Deps) string {
	return popDataDir(d, "work")
}

// LegacyQueueDataDir returns the retired <data>/pop/queue directory. Nothing pop
// writes lives there any more: the supervisor's lock and log moved to
// WorkDataDir and the managed-worktree root moved to <data>/pop/work/worktrees,
// both with the `pop queue` → `pop work` cut. It survives as the pre-cut path
// the lock handover still reads, so a daemon started by a pre-cut binary is seen
// (supervisor_lock.go).
func LegacyQueueDataDir(d *tasks.Deps) string {
	return popDataDir(d, "queue")
}

// popDataDir resolves one of pop's data subdirectories through the same three
// branches every pop data path uses: XDG_DATA_HOME, then the home directory,
// then /tmp when the home directory is unknowable.
func popDataDir(d *tasks.Deps, name string) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", name)
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", name)
	}
	return filepath.Join(home, ".local", "share", "pop", name)
}
