// Package binding owns the Worktree binding model: the durable 1:1 association
// between a Task set (within a Repository identity) and the checkout it drains
// in. Bindings used to live in Queue daemon-private state (ADR-0031); ADR-0036
// moved them into a shared per-repository binding store, and ADR-0055 folds that
// store into pop's global execution-state database (the `bindings` table) so
// that both `pop queue run` (the AFK provisioner) and `pop tasks implement`
// (the attended adopter) can read and write the same association without a
// daemon process running. This module wraps the store-backed accessors in its
// own Store/Binding façade, so its callers are unchanged by the move off the
// retired bindings.json file.
//
// The store keys bindings by Repository identity plus Task set identifier and
// records, per binding, whether pop provisioned the checkout (`git worktree
// add`) or merely adopted an existing one. Provisioned bindings are torn down
// on integration/abandon; adopted bindings are never deleted — only the
// association is forgotten.
package binding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// keySeparator joins a repository key and Task set identifier into a store key.
// It is a NUL byte so it can never collide with a path or set identifier.
const keySeparator = "\x00"

// RepoKey returns the repository-identity prefix used in set-scoped store keys.
func RepoKey(id *tasks.RepositoryIdentity) string {
	return id.Basename + "-" + id.ShortHash
}

// ScopedKey joins a repository key and Task set identifier into a store key.
func ScopedKey(repoKey, setID string) string {
	return repoKey + keySeparator + setID
}

// Key builds the store key for one (Repository identity, Task set identifier)
// pair. It is the single key shape every caller of the store addresses, so
// `pop queue run` and `pop tasks implement` resolve the same binding for the
// same (repo, set).
func Key(id *tasks.RepositoryIdentity, setID string) string {
	return ScopedKey(RepoKey(id), setID)
}

// SetIDFromKey extracts the Task set identifier from a scoped store key built
// by ScopedKey. It is the inverse of ScopedKey and returns "" for any key not
// in scoped (repoKey + setID) form. This module owns the key shape, so it owns
// the split too — callers must not re-derive it.
func SetIDFromKey(key string) string {
	parts := strings.Split(key, keySeparator)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// Binding records the durable checkout associated with one Task set. It is
// store.Binding directly — the sole Worktree-binding type in the codebase, with
// no converter layer between this package and the store (ADR-0118). ScopedKey
// rides along unused by this package's own map access, which keys by the map
// key instead; Load/Save reconcile the two through the store's whole-set
// rewrite.
//
// Provisioned is true when pop ran `git worktree add` to create the checkout.
// False (or absent) means the binding is adopted — a human pointed an
// existing checkout at the set; pop must never delete it.
type Binding = store.Binding

// Store is the in-memory façade over the shared binding store. It is keyed by
// Repository identity plus Task set identifier (the caller builds the key);
// Load/Save back it onto the `bindings` table in pop's global execution-state
// database (ADR-0055), readable and writable with no daemon process running.
type Store struct {
	Bindings map[string]Binding
}

// Get returns the binding stored under key.
func (s *Store) Get(key string) (Binding, bool) {
	if s == nil || s.Bindings == nil {
		return Binding{}, false
	}
	b, ok := s.Bindings[key]
	return b, ok
}

// Put records binding under key, allocating the map on demand.
func (s *Store) Put(key string, b Binding) {
	if s.Bindings == nil {
		s.Bindings = map[string]Binding{}
	}
	s.Bindings[key] = b
}

// Delete forgets the binding under key.
func (s *Store) Delete(key string) {
	delete(s.Bindings, key)
}

// Provisioned reports whether the binding under key was provisioned by pop
// (safe to teardown) rather than adopted.
func (s *Store) Provisioned(key string) bool {
	b, ok := s.Get(key)
	return ok && b.Provisioned
}

// ShouldTeardown reports whether the checkout under key may have its directory
// removed. It returns true when no binding is recorded (legacy/unknown — pop
// probably created it) or when the binding is explicitly provisioned, and false
// only for explicitly adopted bindings, which must never be deleted.
func (s *Store) ShouldTeardown(key string) bool {
	b, ok := s.Get(key)
	if !ok {
		return true // no binding recorded: legacy path, tear down
	}
	return b.Provisioned // adopted=false → retain; provisioned=true → tear down
}

// ManagedWorktreesRoot returns the directory under which pop-provisioned
// (managed) worktrees live: <pop data dir>/queue/worktrees. It is the single
// fork-base layout shared by every explicit provisioner — the Queue, the Drain
// target picker, and `pop tasks implement --in-worktree` — so a worktree any of
// them creates lands in the same tree and integration/teardown find it.
func ManagedWorktreesRoot(d *tasks.Deps) string {
	return filepath.Join(filepath.Dir(tasks.TaskStorageRoot(d)), "queue", "worktrees")
}

// Load reads the shared binding store from the global execution-state database
// (ADR-0055) through the process-cached store accessor. A store that does not
// yet exist yields an empty Store; the retired bindings.json is migrated into
// the store on first read and is never read here directly. Callers need no
// daemon and no pre-seeding.
func Load(d *tasks.Deps) (*Store, error) {
	if err := migrateLegacyBindingsFile(d); err != nil {
		return nil, err
	}
	s, ok, err := d.Store(false)
	if err != nil || !ok {
		return &Store{}, err
	}
	rows, err := s.AllBindings()
	if err != nil {
		return nil, err
	}
	return &Store{Bindings: rows}, nil
}

// Save writes the shared binding store to the global execution-state database,
// replacing every row in one transaction (ADR-0055), through the process-cached
// store accessor.
func Save(d *tasks.Deps, st *Store) error {
	if st == nil {
		st = &Store{}
	}
	s, _, err := d.Store(true)
	if err != nil {
		return err
	}
	return s.ReplaceAllBindings(st.Bindings)
}

// legacyBindingsFile is the standalone JSON binding store ADR-0055 retires. Its
// contents are folded into the global store on first read, then the file is
// removed. It lived beside the per-repo storage tree, in pop's data dir.
const legacyBindingsFile = "bindings.json"

// LegacyBindingsPath returns the retired standalone binding store file path.
func LegacyBindingsPath(d *tasks.Deps) string {
	return filepath.Join(filepath.Dir(tasks.TaskStorageRoot(d)), legacyBindingsFile)
}

// migrateLegacyBindingsFile folds a surviving bindings.json into the store and
// removes the file. A missing file is the steady state after the one-time
// migration and costs only the read miss — no store is opened. Every active
// binding and its provisioned bit is preserved; an entry already present in the
// store is left untouched (the store wins).
func migrateLegacyBindingsFile(d *tasks.Deps) error {
	path := LegacyBindingsPath(d)
	data, err := d.FS.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var legacy struct {
		Bindings map[string]struct {
			RuntimePath string `json:"runtime_path"`
			Branch      string `json:"branch"`
			Project     string `json:"project"`
			Provisioned bool   `json:"provisioned"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("parse legacy bindings file: %w", err)
	}
	if len(legacy.Bindings) > 0 {
		s, _, err := d.Store(true)
		if err != nil {
			return err
		}
		existing, err := s.AllBindings()
		if err != nil {
			return err
		}
		for key, b := range legacy.Bindings {
			if _, ok := existing[key]; ok {
				continue
			}
			if err := s.PutBinding(store.Binding{
				ScopedKey:   key,
				RuntimePath: b.RuntimePath,
				Branch:      b.Branch,
				Project:     b.Project,
				Provisioned: b.Provisioned,
			}); err != nil {
				return err
			}
		}
	}
	// Retire the file once its contents are safely in the store.
	return d.FS.RemoveAll(path)
}

// Adopt builds an adopted binding record (Provisioned=false) for an existing
// checkout a human pointed at a set. Adopted checkouts are never deleted on
// teardown — only the association is forgotten. The caller persists the record.
func Adopt(checkoutPath, branch, proj string) Binding {
	return Binding{RuntimePath: checkoutPath, Branch: branch, Project: proj, Provisioned: false}
}

// AdoptCurrentCheckout records an adopted Worktree binding for setID pointing at
// checkoutPath when that checkout is a linked git worktree (a non-trunk
// checkout). It is the entry point `pop tasks implement` uses to adopt the
// checkout it runs in (ADR-0036): the binding is identical in shape to a
// `bind-worktree` adoption (Provisioned=false, never deleted), routed through
// this module's shared store. It never runs `git worktree add` — routing never
// provisions (ADR-0052); provisioning is an explicit act handled elsewhere.
//
// It is a no-op returning (false, nil) in two cases. First, when checkoutPath is
// the repository's main working tree (trunk): a trunk drain is never
// integrateable, so it gets no worktree binding. Second, when a binding already
// exists for (repo, set): an `implement` the Queue spawned into a provisioned
// (managed) worktree must never clobber the Queue's binding, and a re-run in an
// already-adopted checkout needs no rewrite. It returns (true, nil) only when it
// records a fresh adopted binding.
func AdoptCurrentCheckout(td *tasks.Deps, pd *project.Deps, cfg *config.Config, projectPath, checkoutPath, setID string) (bool, error) {
	if td == nil {
		return false, fmt.Errorf("missing task dependencies")
	}
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return false, nil
	}
	if checkoutPath == "" {
		checkoutPath = projectPath
	}

	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		return false, err
	}
	key := Key(id, setID)

	bindings, err := Load(td)
	if err != nil {
		return false, err
	}
	if _, ok := bindings.Get(key); ok {
		// Already bound — managed (Queue-provisioned) or adopted. Never clobber:
		// a managed binding owns teardown the adopter must not silently disown.
		return false, nil
	}

	linked, err := IsLinkedWorktree(td, checkoutPath)
	if err != nil {
		return false, err
	}
	if !linked {
		// Trunk: no worktree binding, the set stays non-integrateable.
		return false, nil
	}

	branch := CurrentBranch(td, checkoutPath)
	bindings.Put(key, Adopt(checkoutPath, branch, DetectProject(pd, td, cfg, id)))
	if err := Save(td, bindings); err != nil {
		return false, err
	}
	return true, nil
}

// DetectProject returns the configured picker project whose repository identity
// matches id, or "" when zero or multiple projects match. It is the shared
// best-effort project labeller for adopted bindings so `bind-worktree` and
// `implement` stamp the same Project value.
func DetectProject(pd *project.Deps, td *tasks.Deps, cfg *config.Config, id *tasks.RepositoryIdentity) string {
	if cfg == nil || pd == nil {
		return ""
	}
	projects, err := tasks.ListPickerProjectsWith(pd, cfg)
	if err != nil {
		return ""
	}
	var matches []string
	for _, p := range projects {
		pid, err := tasks.ResolveRepositoryIdentity(td, p.Path)
		if err != nil {
			continue
		}
		if pid.ShortHash == id.ShortHash && pid.Basename == id.Basename {
			matches = append(matches, p.Name)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

// IsLinkedWorktree reports whether checkoutPath is a linked git worktree (a
// non-trunk checkout) rather than the repository's main working tree. A linked
// worktree's git dir lives under the common dir's worktrees/; the main working
// tree's git dir IS the common dir. A bare repo has no main working tree, so
// every checkout reads as linked — correct, since a bare repo has no trunk.
//
// It is the single predicate that decides where a `pop tasks implement` run
// lands: a linked worktree is adopted (integrateable), trunk drains inline. So
// `pop tasks status` reuses it to report that destination before a drain starts.
func IsLinkedWorktree(td *tasks.Deps, checkoutPath string) (bool, error) {
	gitDir, err := gitRevParsePath(td, checkoutPath, "--git-dir")
	if err != nil {
		return false, err
	}
	commonDir, err := gitRevParsePath(td, checkoutPath, "--git-common-dir")
	if err != nil {
		return false, err
	}
	return gitDir != commonDir, nil
}

// gitRevParsePath runs `git rev-parse <which>` in checkoutPath and returns the
// result as a cleaned absolute path so two rev-parse results compare reliably.
func gitRevParsePath(td *tasks.Deps, checkoutPath, which string) (string, error) {
	out, err := td.Git.CommandInDir(checkoutPath, "rev-parse", which)
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", which, err)
	}
	p := strings.TrimSpace(out)
	if p == "" {
		return "", fmt.Errorf("git rev-parse %s: empty path", which)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(checkoutPath, p)
	}
	return filepath.Clean(p), nil
}

// CurrentBranch returns the checked-out branch of checkoutPath, or "" when the
// checkout is detached. A detached worktree still adopts; integration resolves
// its ref later.
func CurrentBranch(td *tasks.Deps, checkoutPath string) string {
	out, err := td.Git.CommandInDir(checkoutPath, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// ProvisionWorktree runs `git worktree add` for setID under worktreesRoot and
// returns the resulting provisioned binding (Provisioned=true). It is the
// Queue's hands-free path; the human-attended path adopts an existing checkout
// instead (see the queue bind-worktree command). The caller persists the
// returned binding into the Store.
func ProvisionWorktree(d *tasks.Deps, worktreesRoot, projectPath, setID string, now time.Time) (Binding, error) {
	if d == nil {
		return Binding{}, fmt.Errorf("missing task dependencies")
	}
	id, err := tasks.ResolveRepositoryIdentity(d, projectPath)
	if err != nil {
		return Binding{}, err
	}
	safeSet := SafeComponent(setID)
	stamp := now.UTC().Format("20060102T150405Z")
	branch := fmt.Sprintf("pop/%s/%s", safeSet, stamp)
	path := filepath.Join(worktreesRoot, id.Basename+"-"+id.ShortHash, safeSet)
	if err := d.FS.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Binding{}, fmt.Errorf("create worktree parent: %w", err)
	}
	if _, err := d.Git.CommandInDir(projectPath, "worktree", "add", "-b", branch, path, "HEAD"); err != nil {
		return Binding{}, fmt.Errorf("git worktree add: %w", err)
	}
	return Binding{RuntimePath: path, Branch: branch, Provisioned: true}, nil
}

// TeardownWorktree removes a provisioned checkout: it detaches the worktree and
// deletes its branch. force selects `git branch -D` over `-d`. It must only be
// called for provisioned bindings; adopted checkouts are never torn down.
func TeardownWorktree(d *tasks.Deps, workingPath, runtimePath, branch string, force bool) error {
	if _, err := d.Git.CommandInDir(workingPath, "worktree", "remove", runtimePath); err != nil {
		return fmt.Errorf("remove worktree %s: %w", runtimePath, err)
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	if _, err := d.Git.CommandInDir(workingPath, "branch", flag, branch); err != nil {
		return fmt.Errorf("delete branch %s: %w", branch, err)
	}
	return nil
}

// SafeComponent sanitises a Task set identifier into a filesystem-safe path
// component used in provisioned worktree directory names.
func SafeComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "set"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "set"
	}
	return out
}
