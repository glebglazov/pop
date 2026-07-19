package config

import (
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/debug"
)

// This file is the shared repo-scope source enumerator (ADR-0122, folding in the
// architecture-review triplication of ADR-0083 repo-scope resolution). One
// enumerator maps a checkout to its ordered repo-scope sources — the
// identity-matched [repo."<path>"] override, the in-tree .pop.toml anchor(s),
// and the runtime entries where legal — doing the repo-identity walk,
// canonicalization, and .pop.toml reads once (caching each anchor by its
// canonical path, the read-once guard). ResolveRepoConfig, ResolveWorkbenchesWith,
// and ResolvePreferredWorkbench consume it instead of each hand-walking identity
// and re-reading .pop.toml.
//
// Enumeration is lazy (ADR-0054): it happens at query time per checkout, a
// source is read only when a resolver asks for it, and a malformed .pop.toml
// degrades to the zero config exactly as before. Walker merges stay same-type:
// the enumerator hands over the embedded RepoScopeConfig, never the outer
// RepoOverrideConfig/RepoConfig — the [repo]-only trunk key stays caller-side
// with its exact-checkout-path condition.

// repoScopeEnumerator resolves the ADR-0083 repo-scope sources for one checkout.
type repoScopeEnumerator struct {
	d            *Deps
	cfg          *Config
	checkoutPath string
	canon        string
	identity     string

	// override is the [repo."<path>"] block whose key shares this checkout's
	// repository identity, resolved once. overrideFound distinguishes "no block"
	// from "a zero block"; overrideKeyCanon is the canonical key path (for
	// collision messages); overrideExact reports whether that key canon equals the
	// checkout canon (the trunk per-checkout gate).
	overrideFound    bool
	override         RepoOverrideConfig
	overrideKeyCanon string
	overrideExact    bool

	// popCache memoizes .pop.toml reads by canonical anchor path (read-once guard).
	popCache map[string]popTOMLRead
}

type popTOMLRead struct {
	cfg RepoConfig
	err error
}

// newRepoScope builds the enumerator for checkoutPath, doing the repo-identity
// walk and the [repo."<path>"] match up front (both cheap, filesystem-stat only)
// and deferring the .pop.toml and runtime reads until a resolver asks for them.
func (c *Config) newRepoScope(d *Deps, checkoutPath string) *repoScopeEnumerator {
	e := &repoScopeEnumerator{
		d:            d,
		cfg:          c,
		checkoutPath: checkoutPath,
		canon:        canonicalPath(d, checkoutPath),
		identity:     repoIdentity(d, checkoutPath),
		popCache:     map[string]popTOMLRead{},
	}
	e.matchOverride()
	return e
}

// matchOverride finds the [repo."<path>"] block whose key shares this checkout's
// repository identity. At most one block matches a given identity in practice;
// when several keys resolve to the same identity the last wins (map order is
// non-deterministic, as it was before the refactor).
func (e *repoScopeEnumerator) matchOverride() {
	if e.cfg == nil {
		return
	}
	for rawKey, block := range e.cfg.Repo {
		if repoIdentity(e.d, rawKey) != e.identity {
			continue
		}
		b := block
		e.override = b
		e.overrideFound = true
		e.overrideKeyCanon = canonicalPath(e.d, rawKey)
		e.overrideExact = e.overrideKeyCanon == e.canon
	}
}

// popTOML reads the committed .pop.toml at anchor, caching by canonical path so
// an anchor shared by several layers (or by two resolvers) is read exactly once.
func (e *repoScopeEnumerator) popTOML(anchor string) (RepoConfig, error) {
	key := canonicalPath(e.d, anchor)
	if r, ok := e.popCache[key]; ok {
		return r.cfg, r.err
	}
	cfg, err := LoadRepoConfigWith(e.d, anchor)
	e.popCache[key] = popTOMLRead{cfg: cfg, err: err}
	return cfg, err
}

// popPreferred reads preferred_workbench from the committed .pop.toml at anchor,
// degrading a malformed file to "" with a debug log (a broken in-tree file must
// not block getting into a session).
func (e *repoScopeEnumerator) popPreferred(anchor string) string {
	cfg, err := e.popTOML(anchor)
	if err != nil {
		debug.Error("config: read .pop.toml preferred workbench at %s: %v", anchor, err)
		return ""
	}
	return cfg.PreferredWorkbench
}

// inheritedAnchor returns the checkout whose committed .pop.toml supplies the
// inherited (layer-4) repo-scope value: the Trunk worktree when the resolver
// reports one, otherwise the repository identity root — where a bare repo's
// shared .pop.toml lives (ADR-0083). Reuses the identity computed once.
func (e *repoScopeEnumerator) inheritedAnchor() string {
	if e.d != nil && e.d.Trunk != nil {
		if trunkPath, ok := e.d.Trunk(e.checkoutPath); ok && trunkPath != "" {
			return trunkPath
		}
	}
	return e.identity
}

// popScopeAnchors returns the in-tree .pop.toml anchors for the checkout in
// merge order (lowest precedence first) under ADR-0083's two-anchor law: the
// trunk anchor (inherited — the Trunk worktree, or the repository-identity root
// for a bare repo) then this worktree, so the worktree's own committed values
// win. The trunk anchor is dropped when it canonicalizes to this very checkout,
// the read-once guard: a checkout that is its own trunk anchor is read (and, for
// workbenches, warned about) exactly once.
func (e *repoScopeEnumerator) popScopeAnchors() []string {
	if inherited := e.inheritedAnchor(); canonicalPath(e.d, inherited) != e.canon {
		return []string{inherited, e.checkoutPath}
	}
	return []string{e.checkoutPath}
}

// resolveRepoConfig returns the effective RepoConfig for the checkout: the
// committed .pop.toml (resolved worktree-first then the trunk anchor, presence
// deciding — ADR-0083) with the identity-matched [repo."<path>"] override
// walker-merged on top (later ladder source wins). The [repo]-only trunk key
// stays caller-side — per-checkout, applied only when the override's key path
// exactly matches this checkout. A missing .pop.toml is not an error; a
// malformed one degrades to the zero config with its error returned.
func (e *repoScopeEnumerator) resolveRepoConfig() (RepoConfig, error) {
	var result RepoConfig
	var popErr error
	// Merge the anchors lowest precedence first (trunk then worktree) so the
	// worktree's committed values win; Findings from each anchor read are kept so
	// scope-legality problems in either file still surface to the picker banner.
	for _, anchor := range e.popScopeAnchors() {
		cfg, err := e.popTOML(anchor)
		if err != nil {
			popErr = err
		}
		src := cfg.RepoScopeConfig
		mergeWalk(&result.RepoScopeConfig, &src, repoScopeMetadata(src), repoScopePolicy())
		result.Findings = append(result.Findings, cfg.Findings...)
	}
	if e.overrideFound {
		src := e.override.RepoScopeConfig
		mergeWalk(&result.RepoScopeConfig, &src, repoScopeMetadata(src), repoScopePolicy())
		if e.override.Trunk != nil && e.overrideExact {
			result.Trunk = *e.override.Trunk
		}
	}
	return result, popErr
}

// preferredSource is one layer of the ADR-0083 preferred_workbench chain,
// highest precedence first. A hand-authored layer carries a resolved name and
// falls through when it is empty; a runtime layer carries the path to read and
// honours the three-valued explicit-none sentinel.
type preferredSource struct {
	runtime     bool
	name        string // hand-authored: the value ("" means unset, fall through)
	runtimePath string // runtime: path whose entry to read
	debugLabel  string // runtime: message stem for a read-error debug log
}

// preferredSources returns the ordered preferred_workbench chain for the
// checkout (ADR-0083). The .pop.toml anchors are read here (cached), so the
// consider-chain iterates a flat list instead of hand-walking anchors; the
// runtime layers stay descriptors read in the chain so their three-valued
// semantics and per-layer debug logs are preserved.
//
//	1  config.toml [repo."<path>"]        hand-authored, central, repo-specific
//	3  ./.pop.toml                        hand-authored, in-tree, this worktree
//	4  <trunk-or-id-root>/.pop.toml       hand-authored, in-tree, inherited
//	5  config.runtime.toml[<wt-path>]     runtime, this worktree
//	6  config.runtime.toml[<trunk-path>]  runtime, inherited from the Trunk
//
// Layer 2 (config.toml global keys) has no home for this key and is omitted.
// Layer 4 is dropped when its anchor is this very checkout, and layer 6 when the
// trunk is absent or is this checkout — the read-once guard, so a stale name is
// never double-warned by re-reading the same anchor.
func (e *repoScopeEnumerator) preferredSources() []preferredSource {
	sources := []preferredSource{
		{name: e.override.PreferredWorkbench},  // layer 1
		{name: e.popPreferred(e.checkoutPath)}, // layer 3
	}

	if anchor := e.inheritedAnchor(); canonicalPath(e.d, anchor) != e.canon {
		sources = append(sources, preferredSource{name: e.popPreferred(anchor)}) // layer 4
	}

	sources = append(sources, preferredSource{ // layer 5
		runtime:     true,
		runtimePath: e.checkoutPath,
		debugLabel:  "runtime preferred workbench",
	})

	if e.d.Trunk != nil {
		if trunkPath, ok := e.d.Trunk(e.checkoutPath); ok && trunkPath != "" &&
			canonicalPath(e.d, trunkPath) != e.canon {
			sources = append(sources, preferredSource{ // layer 6
				runtime:     true,
				runtimePath: trunkPath,
				debugLabel:  "trunk preferred workbench",
			})
		}
	}

	return sources
}

// repoScopeMetadata synthesizes a toml.MetaData whose defined keys mirror the
// non-zero fields of a RepoScopeConfig, so the walker treats an empty slice or
// empty string as "unset" — the same presence rule the pre-walker repo-scope
// code used (a non-empty override value wins; an empty one leaves the base). It
// is built from a minimal TOML string (only key presence matters, not the
// values), avoiding the encoder's emit-empty-scalar behaviour.
func repoScopeMetadata(scope RepoScopeConfig) toml.MetaData {
	var b strings.Builder
	if scope.PreferredWorkbench != "" {
		b.WriteString("preferred_workbench = \"x\"\n")
	}
	if len(scope.Workbenches) > 0 {
		b.WriteString("workbenches = []\n")
	}
	var probe RepoScopeConfig
	md, _ := toml.Decode(b.String(), &probe)
	return md
}

// workbenchCollisionName extracts the workbench name from the walker's
// list-by-key collision key path ("workbenches[<name>]").
func workbenchCollisionName(keyPath string) string {
	name := strings.TrimPrefix(keyPath, "workbenches[")
	return strings.TrimSuffix(name, "]")
}
