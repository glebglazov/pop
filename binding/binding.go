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

	"github.com/glebglazov/pop/tasks"
)

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
func Adopt(checkoutPath, branch, project string) Binding {
	return Binding{RuntimePath: checkoutPath, Branch: branch, Project: project, Provisioned: false}
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
