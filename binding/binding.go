// Package binding owns the Worktree binding model: the durable 1:1 association
// between a Task set (within a Repository identity) and the checkout it drains
// in. Bindings used to live in Queue daemon-private state (ADR-0031); ADR-0036
// moves them into a shared per-repository drain store owned by this module so
// that both `pop queue run` (the AFK provisioner) and `pop tasks implement`
// (the attended adopter) can read and write the same association without a
// daemon process running.
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

// Binding records the durable checkout associated with one Task set.
type Binding struct {
	RuntimePath string `json:"runtime_path"`
	Branch      string `json:"branch"`
	Project     string `json:"project"`
	// Provisioned is true when pop ran `git worktree add` to create this
	// checkout. False (or absent) means the binding is adopted — a human
	// pointed an existing checkout at the set; pop must never delete it.
	Provisioned bool `json:"provisioned,omitempty"`
}

// Store is the shared per-repository binding store. It is keyed by Repository
// identity plus Task set identifier (the caller builds the key) and persists to
// a single JSON file outside daemon-private state, readable and writable with
// no daemon process running.
type Store struct {
	Bindings map[string]Binding `json:"bindings,omitempty"`
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

// StorePath returns the shared binding store file path. It lives beside the
// per-repository Task storage root (pop's data dir), not inside daemon state.
func StorePath(d *tasks.Deps) string {
	return filepath.Join(filepath.Dir(tasks.TaskStorageRoot(d)), "bindings.json")
}

// Load reads the shared binding store. A missing file yields an empty store
// rather than an error so callers need no daemon and no pre-seeding.
func Load(d *tasks.Deps) (*Store, error) {
	data, err := d.FS.ReadFile(StorePath(d))
	if errors.Is(err, os.ErrNotExist) {
		return &Store{}, nil
	}
	if err != nil {
		return nil, err
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse binding store: %w", err)
	}
	return &store, nil
}

// Save writes the shared binding store atomically.
func Save(d *tasks.Deps, store *Store) error {
	if store == nil {
		store = &Store{}
	}
	payload, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return tasks.WriteAtomicWith(d, StorePath(d), append(payload, '\n'), 0o644)
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
// this module's shared store. It never runs `git worktree add` — provisioning
// stays the Queue's path, gated by worktree_ready.
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

	store, err := Load(td)
	if err != nil {
		return false, err
	}
	if _, ok := store.Get(key); ok {
		// Already bound — managed (Queue-provisioned) or adopted. Never clobber:
		// a managed binding owns teardown the adopter must not silently disown.
		return false, nil
	}

	linked, err := isLinkedWorktree(td, checkoutPath)
	if err != nil {
		return false, err
	}
	if !linked {
		// Trunk: no worktree binding, the set stays non-integrateable.
		return false, nil
	}

	branch := currentBranch(td, checkoutPath)
	store.Put(key, Adopt(checkoutPath, branch, DetectProject(pd, td, cfg, id)))
	if err := Save(td, store); err != nil {
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

// isLinkedWorktree reports whether checkoutPath is a linked git worktree (a
// non-trunk checkout) rather than the repository's main working tree. A linked
// worktree's git dir lives under the common dir's worktrees/; the main working
// tree's git dir IS the common dir. A bare repo has no main working tree, so
// every checkout reads as linked — correct, since a bare repo has no trunk.
func isLinkedWorktree(td *tasks.Deps, checkoutPath string) (bool, error) {
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

// currentBranch returns the checked-out branch of checkoutPath, or "" when the
// checkout is detached. A detached worktree still adopts; integration resolves
// its ref later.
func currentBranch(td *tasks.Deps, checkoutPath string) string {
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
