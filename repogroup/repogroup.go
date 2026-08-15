// Package repogroup resolves the repository groups that can contribute Work:
// every repository with a Task-storage marker on disk, paired with the config
// projects whose checkout nests under it, its integration target, and the
// coordinates a kind needs to read state for it. Resolution is fork-free —
// identity, integration target and branch all come from the repo.json marker,
// config, and a HEAD file read (ADR-0060).
//
// It sits below the Work kinds rather than inside one: the Task-set kind and the
// Map kind both scan per repository group, and the group resolution needs
// tasks/binding, which imports tasks — so it can live in neither kind package.
package repogroup

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// Deps holds what resolution reads: the Task-storage side (markers, identity,
// canonical paths) and the picker side (config projects). The store handle is
// borrowed through Tasks in if-exists mode (ADR-0140) — a pure read never
// materialises a database and the handle is never closed here.
type Deps struct {
	Tasks   *tasks.Deps
	Project *project.Deps
}

// Checkout holds the minimal per-checkout coordinates a group carries: the picker
// labels and the canonical checkout path.
type Checkout struct {
	Name         string
	ProjectLabel string
	ProjectPath  string
	RuntimePath  string
}

// Group is one repository group's static resolution: the repository coordinates
// and integration target, all derived fork-free from the repo.json marker's
// common directory and config (ADR-0060). Kinds recompute only their volatile
// overlay (statuses, locks, daemon state) per read.
type Group struct {
	DefPath       string
	StatePath     string
	StorageDir    string
	RepoKey       string
	RepoCommonDir string
	ProjectName   string
	// Rep is the integration target — the checkout every bind/drain sub-action
	// runs git against. Nil only for a bare repo with no resolvable target.
	Rep    *Checkout
	Branch string
	Bare   bool
	// ConfigError is non-empty when the repository cannot resolve an integration
	// target from config — a bare repo with no declared trunk (ADR-0060/0059). Its
	// containers render this as a config-class error rather than forking git.
	ConfigError string
}

// CheckoutPath returns the group's representative checkout, the path every
// bind/drain sub-action runs git against. It is empty only for a bare repo with
// no resolvable representative.
func (g Group) CheckoutPath() string {
	if g.Rep == nil {
		return ""
	}
	return g.Rep.ProjectPath
}

// Storage returns the group's Task-storage directory, falling back to the
// definition path's parent for a group resolved without one.
func (g Group) Storage() string {
	if g.StorageDir != "" {
		return g.StorageDir
	}
	if g.DefPath != "" {
		return filepath.Dir(g.DefPath)
	}
	return ""
}

// ScanReason is the config-class message a bare repo with no declared trunk
// carries on its containers (ADR-0059/0060).
const ScanReason = "needs trunk; skipped (set trunk = \"<path>\" in a global [repo] block)"

// Resolve resolves every renderable repo group's static coordinates fork-free
// (ADR-0060). It iterates the repositories that have a Task storage marker on
// disk — the only repos that can contribute Work — and pairs each with the config
// projects whose checkout nests under (or contains) its working-tree root. A
// repository with no matching config project is dropped (ADR-0042).
func Resolve(d *Deps, cfg *config.Config) ([]Group, error) {
	if d == nil {
		return nil, errors.New("repogroup: nil deps")
	}
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return nil, err
	}
	// Work discovery includes wayfinder-only storage (ADR-0130).
	repos, err := tasks.ListWorkStorageRepos(d.Tasks)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil
	}

	type candidate struct {
		p     project.ExpandedProject
		canon string
	}
	cands := make([]candidate, 0, len(projects))
	for _, p := range projects {
		canon, err := canonicalCheckoutPath(d.Tasks, p.Path)
		if err != nil {
			canon = p.Path
		}
		cands = append(cands, candidate{p: p, canon: canon})
	}

	groups := make([]Group, 0, len(repos))
	for _, repo := range repos {
		root := storageRepoRoot(d.Tasks, repo.RepositoryPath)
		var scans []Checkout
		for _, c := range cands {
			if pathWithinOrEqual(c.canon, root) || pathWithinOrEqual(root, c.canon) {
				scans = append(scans, Checkout{
					Name:         c.p.Name,
					ProjectLabel: c.p.ProjectLabel,
					ProjectPath:  c.canon,
					RuntimePath:  c.canon,
				})
			}
		}
		if len(scans) == 0 {
			continue // registered storage but not in config: dropped by the intersection.
		}
		g, err := FromMarker(d, cfg, repo.RepositoryPath, scans)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// FromMarker derives one repo group's static coordinates from its marker's common
// directory and config, forking no git (ADR-0060): identity and paths come from
// IdentityFromCommonDir, the integration target from representative, and the
// branch from a HEAD file read. A bare repo with no declared trunk carries a
// config-class error on ConfigError instead.
func FromMarker(d *Deps, cfg *config.Config, commonDir string, scans []Checkout) (Group, error) {
	id, err := tasks.IdentityFromCommonDir(d.Tasks, commonDir)
	if err != nil {
		return Group{}, err
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
	if err != nil {
		return Group{}, err
	}

	rep, bare, err := representative(d, cfg, id.CommonDir, scans)
	if err != nil {
		return Group{}, err
	}
	branch := ""
	configErr := ""
	switch {
	case rep != nil:
		branch = HeadBranch(d.Tasks, rep.ProjectPath, id.CommonDir)
	case bare:
		// Bare repo with no declared trunk: no integration target to fork for.
		configErr = ScanReason
	}

	return Group{
		DefPath:       defPath,
		StatePath:     tasks.StatePathFor(defPath),
		StorageDir:    id.StorageDir,
		RepoKey:       binding.RepoKey(id),
		RepoCommonDir: id.CommonDir,
		ProjectName:   repoName(scans, rep),
		Rep:           rep,
		Branch:        branch,
		Bare:          bare,
		ConfigError:   configErr,
	}, nil
}

// representative resolves a repo group's integration target without forking git
// (ADR-0060): the checkout the repository states as its trunk wins (bare or
// not), else a non-bare repo's target is the main worktree — the parent of the common
// directory — and a bare repo with no declared trunk has none (bare=true,
// rep=nil). A renamed execution key surfaces as a fatal config finding.
func representative(d *Deps, cfg *config.Config, commonDir string, scans []Checkout) (*Checkout, bool, error) {
	if cfg != nil && len(scans) > 0 {
		if _, err := resolveRepoConfigFor(d, cfg, scans[0].ProjectPath); err != nil {
			var f config.Finding
			if errors.As(err, &f) {
				return nil, false, err
			}
		}
	}

	// 1. the checkout the repository states as its trunk (config-only, no git).
	for i := range scans {
		rc, err := resolveRepoConfigFor(d, cfg, scans[i].ProjectPath)
		if err == nil && rc.IsTrunk(configDepsFor(d), scans[i].ProjectPath) {
			return &scans[i], false, nil
		}
	}

	// 2. non-bare repo → main worktree = parent of the common directory.
	if filepath.Base(commonDir) == ".git" {
		return scanForCheckout(d, scans, filepath.Dir(commonDir)), false, nil
	}

	// 3. bare repo with no declared trunk → no integration target.
	return nil, true, nil
}

// scanForCheckout returns the checkout that canonicalizes to checkoutPath, or
// synthesizes one (fork-free) when the target — e.g. a main worktree that is not
// itself a picker Project — is not among the group's checkouts.
func scanForCheckout(d *Deps, scans []Checkout, checkoutPath string) *Checkout {
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
	return &Checkout{
		Name:         name,
		ProjectLabel: label,
		ProjectPath:  canon,
		RuntimePath:  canon,
	}
}

// HeadBranch reads a checkout's current branch from its HEAD file — no `git
// branch --show-current` (ADR-0060). It resolves the checkout's git directory (a
// `.git` directory for a main worktree, or the `gitdir:` pointer in a linked
// worktree's `.git` file), falling back to commonDir, then parses `ref:
// refs/heads/<branch>`. A detached HEAD or any read failure yields "".
func HeadBranch(d *tasks.Deps, checkout, commonDir string) string {
	gitDir := ""
	if strings.TrimSpace(checkout) != "" {
		dotGit := filepath.Join(checkout, ".git")
		if info, err := d.FS.Stat(dotGit); err == nil {
			if info.IsDir() {
				gitDir = dotGit
			} else if data, rerr := d.FS.ReadFile(dotGit); rerr == nil {
				line := strings.TrimSpace(string(data))
				if p := strings.TrimPrefix(line, "gitdir:"); p != line {
					p = strings.TrimSpace(p)
					if !filepath.IsAbs(p) {
						p = filepath.Join(checkout, p)
					}
					gitDir = filepath.Clean(p)
				}
			}
		}
	}
	if gitDir == "" {
		gitDir = commonDir
	}
	if strings.TrimSpace(gitDir) == "" {
		return ""
	}
	data, err := d.FS.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if ref := strings.TrimPrefix(head, "ref: refs/heads/"); ref != head {
		return strings.TrimSpace(ref)
	}
	return ""
}

// storageRepoRoot derives a repository's working-tree root from the canonical git
// common directory recorded in its marker: a normal repo's common dir is
// `<root>/.git` and a bare-with-worktrees layout's is `<root>/.bare`, so the root
// is the parent; a top-level bare repo's common dir is the repo dir itself.
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

// canonicalCheckoutPath resolves an absolute, symlink-evaluated checkout path.
func canonicalCheckoutPath(d *tasks.Deps, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return d.FS.EvalSymlinks(abs)
}

// resolveRepoConfigFor resolves the effective RepoConfig for a checkout, merging
// global [repo."<path>"] overrides over repo-root .pop/config.toml. trunk is
// honored only for the keyed checkout path.
func resolveRepoConfigFor(d *Deps, cfg *config.Config, checkoutPath string) (config.RepoConfig, error) {
	cd := configDepsFor(d)
	if cfg == nil {
		return config.LoadRepoConfigWith(cd, checkoutPath)
	}
	return cfg.ResolveRepoConfig(cd, checkoutPath)
}

// configDepsFor is the config-layer view of these deps: the same filesystem, so a
// config read here is stubbed by whatever the test stubbed the scan with.
func configDepsFor(d *Deps) *config.Deps {
	pd := d.Project
	if pd == nil || pd.FS == nil {
		pd = project.DefaultDeps()
	}
	return &config.Deps{FS: pd.FS}
}

// repoName derives a stable label for a repository unit — the repository display
// label (ProjectLabel), so a bare repo's representative shows "game server"
// rather than "game server/main". It falls back to the full picker Name when no
// ProjectLabel is carried (e.g. synthesized checkouts).
func repoName(scans []Checkout, rep *Checkout) string {
	if rep != nil {
		if rep.ProjectLabel != "" {
			return rep.ProjectLabel
		}
		if rep.Name != "" {
			return rep.Name
		}
	}
	if len(scans) > 0 {
		if scans[0].ProjectLabel != "" {
			return scans[0].ProjectLabel
		}
		return scans[0].Name
	}
	return ""
}
