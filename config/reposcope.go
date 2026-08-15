package config

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/debug"
)

// This file is the shared repo-scope source enumerator (ADR-0122, folding in the
// architecture-review triplication of repo-scope resolution). One enumerator maps
// a checkout to its ordered repo-scope sources — the global config's own values
// for repo-scope keys, the settings pop recorded for the repository, the in-tree
// .pop/config.toml anchor(s), and the identity-matched [repo."<path>"] block —
// doing the repo-identity walk, canonicalization, and .pop/config.toml reads once
// (caching each anchor by its canonical path, the read-once guard).
// ResolveRepoConfig, ResolveWorkbenchesWith, and ResolvePreferredWorkbench consume
// it instead of each hand-walking identity and re-reading .pop/config.toml.
//
// The order it hands over is the scope-first law (ADR-0212 decision 1), spelled
// out on scopeLadder: the most specific scope wins, and within one scope a
// declaration — hand-authored or committed — beats a gap-filler pop wrote.
// Nothing here ranks a source by who authored its file, which is what ADR-0083's
// superseded law did; the walker that consumes the order is unchanged.
//
// Enumeration is lazy (ADR-0054): it happens at query time per checkout, a
// source is read only when a resolver asks for it, and a malformed .pop/config.toml
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

	// popCache memoizes .pop/config.toml reads by canonical anchor path (read-once guard).
	popCache map[string]popTOMLRead
}

type popTOMLRead struct {
	cfg RepoConfig
	err error
}

// newRepoScope builds the enumerator for checkoutPath, doing the repo-identity
// walk and the [repo."<path>"] match up front (both cheap, filesystem-stat only)
// and deferring the .pop/config.toml and runtime reads until a resolver asks for them.
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

// popTOML reads the committed .pop/config.toml at anchor, caching by canonical path so
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

// popPreferred reads preferred_workbench from the committed .pop/config.toml at anchor,
// degrading a malformed file to "" with a debug log (a broken in-tree file must
// not block getting into a session).
func (e *repoScopeEnumerator) popPreferred(anchor string) string {
	cfg, err := e.popTOML(anchor)
	if err != nil {
		debug.Error("config: read .pop/config.toml preferred workbench at %s: %v", anchor, err)
		return ""
	}
	return cfg.PreferredWorkbench
}

// inheritedAnchor returns the checkout whose committed .pop/config.toml supplies the
// inherited (layer-4) repo-scope value: the Trunk worktree when the resolver
// reports one, otherwise the repository identity root — where a bare repo's
// shared .pop/config.toml lives (ADR-0083). Reuses the identity computed once.
func (e *repoScopeEnumerator) inheritedAnchor() string {
	if e.d != nil && e.d.Trunk != nil {
		if trunkPath, ok := e.d.Trunk(e.checkoutPath); ok && trunkPath != "" {
			return trunkPath
		}
	}
	return e.identity
}

// popScopeAnchors returns the in-tree .pop/config.toml anchors for the checkout in
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

// repoScopeSource is one rung of the scope-first ladder: what a single source
// declares for the shared repo-scope key set, plus what a reader needs to name it
// and to hear about a problem reading it.
type repoScopeSource struct {
	// label names the source in a collision warning ("global config",
	// ".pop/config.toml", `[repo."<canon>"]`).
	label string
	// scope is this source's values for the shared repo-scope key set.
	scope RepoScopeConfig
	// findings and err carry a committed anchor's scope-legality problems and its
	// read error, so a malformed or illegal in-tree file still reaches the picker
	// banner rather than being swallowed by the merge.
	findings []Finding
	err      error
	// gapFiller marks the one rung that is not a declaration: the settings pop
	// recorded for this repository. Everything pop records today is a [repo]-only
	// key (turn_cap), which lives outside RepoScopeConfig, so this rung's scope is
	// empty and only resolveRepoConfig acts on it — reading it lazily, so
	// resolving workbenches costs no extra file read.
	gapFiller bool
}

// scopeLadder returns this checkout's repo-scope sources in merge order (lowest
// precedence first). It is the scope-first law of ADR-0212 decision 1 written as
// one list, and it is the only place the order lives:
//
//	global     · declaration   config.toml's own values for repo-scope keys
//	repository · gap-filler    the settings pop recorded for this repository
//	repository · declaration   <trunk-or-id-root>/.pop/config.toml   (inherited)
//	repository · declaration   ./.pop/config.toml                    (this worktree)
//	repository · declaration   config.toml [repo."<path>"]           (this checkout)
//
// The most specific scope wins, so a team's committed .pop/config.toml now beats
// a personal global value for the same key — the reversal of ADR-0083, which
// ranked by authorship and let the general statement shadow the specific one.
// Within one scope a declaration beats the gap-filler, which is why a turn cap
// written by hand still wins over one pop recorded. The checkout-keyed
// [repo."<path>"] block is the most specific declaration there is and so still
// beats every source below it.
//
// Only keys that have a global home carry a value on the global rung;
// preferred_workbench has none, so for that key the ladder simply starts at the
// repository.
func (e *repoScopeEnumerator) scopeLadder() []repoScopeSource {
	ladder := []repoScopeSource{
		{label: "global config", scope: e.globalScope()},
		{label: "pop-written repo settings", gapFiller: true},
	}
	// The committed anchors merge inherited-first (trunk then worktree) so the
	// worktree's own .pop/config.toml outranks the one it inherits (ADR-0083's
	// surviving two-anchor law).
	for _, anchor := range e.popScopeAnchors() {
		cfg, err := e.popTOML(anchor)
		ladder = append(ladder, repoScopeSource{
			label:    ".pop/config.toml",
			scope:    cfg.RepoScopeConfig,
			findings: cfg.Findings,
			err:      err,
		})
	}
	if e.overrideFound {
		ladder = append(ladder, repoScopeSource{
			label: fmt.Sprintf("[repo.%q]", e.overrideKeyCanon),
			scope: e.override.RepoScopeConfig,
		})
	}
	return ladder
}

// globalScope is what the global config.toml declares for the repo-scope key set
// — its bottom rung. Workbenches is the one shared key with a global home (the
// global blueprint library); a key with no global spelling contributes nothing
// and the zero value reads as "unset" to the walker.
func (e *repoScopeEnumerator) globalScope() RepoScopeConfig {
	if e.cfg == nil {
		return RepoScopeConfig{}
	}
	return RepoScopeConfig{Workbenches: e.cfg.Workbenches}
}

// resolveRepoConfig returns the effective RepoConfig for the checkout: the
// scope-first ladder walked lowest rung first, so the most specific source that
// declares a key supplies it. The [repo]-only keys stay caller-side — trunk is
// per-checkout, applied only when the override's key path exactly matches this
// checkout; turn_cap is per-repository. A missing .pop/config.toml is not an
// error; a malformed one degrades to the zero config with its error returned.
func (e *repoScopeEnumerator) resolveRepoConfig() (RepoConfig, error) {
	var result RepoConfig
	var popErr error
	for _, src := range e.scopeLadder() {
		if src.gapFiller {
			e.applyRecordedSettings(&result)
			continue
		}
		if src.err != nil {
			popErr = src.err
		}
		scope := src.scope
		mergeWalk(&result.RepoScopeConfig, &scope, repoScopeMetadata(scope), repoScopePolicy())
		result.Findings = append(result.Findings, src.findings...)
	}
	if e.overrideFound {
		if e.override.Trunk != nil && e.overrideExact {
			result.Trunk = *e.override.Trunk
		}
		// turn_cap deliberately skips trunk's exact-checkout gate: the block was
		// matched by repository identity, so every worktree of the repository reads
		// the one bound (ADR-0191). A non-positive number bounds nothing, so it
		// reads as "declares no cap" rather than as a cap of zero turns — and a
		// hand-written one still overrides a bound pop recorded, which is
		// declaration-beats-gap-filler within the repository scope.
		if e.override.TurnCap != nil {
			result.TurnCap = 0
			if *e.override.TurnCap > 0 {
				result.TurnCap = *e.override.TurnCap
			}
		}
	}
	return result, popErr
}

// applyRecordedSettings folds in the repository gap-filler: the settings pop
// recorded for this repository, keyed by repository identity so one record serves
// every worktree (ADR-0191). It is applied at its rung of the ladder — above the
// global declarations, below every repository declaration — so a declaration of
// the same key, wherever in the repository it was made, overwrites it. A broken
// runtime file degrades to "pop recorded nothing" rather than failing the whole
// resolution.
func (e *repoScopeEnumerator) applyRecordedSettings(result *RepoConfig) {
	stored, _, err := runtimeRepoSettingsWith(e.d, e.identity)
	if err != nil {
		debug.Error("config: read pop-written repo settings for %s: %v", e.identity, err)
		return
	}
	if stored.TurnCap != nil && *stored.TurnCap > 0 {
		result.TurnCap = *stored.TurnCap
	}
}

// preferredSource is one rung of the preferred_workbench chain, highest
// precedence first. A declaration rung carries a resolved name and falls through
// when it is empty; a runtime rung carries the path to read and honours the
// three-valued explicit-none sentinel.
type preferredSource struct {
	runtime     bool
	name        string // hand-authored: the value ("" means unset, fall through)
	runtimePath string // runtime: path whose entry to read
	debugLabel  string // runtime: message stem for a read-error debug log
}

// preferredSources returns the ordered preferred_workbench chain for the
// checkout — scopeLadder's law read from the top, for the one key whose runtime
// rungs are keyed by checkout rather than by repository identity. The
// .pop/config.toml anchors are read here (cached), so the consider-chain iterates
// a flat list instead of hand-walking anchors; the runtime rungs stay descriptors
// read in the chain so their three-valued semantics and per-rung debug logs are
// preserved. Highest precedence first:
//
//	repository · declaration   config.toml [repo."<path>"]        this checkout
//	repository · declaration   ./.pop/config.toml                 this worktree
//	repository · declaration   <trunk-or-id-root>/.pop/config.toml  inherited
//	repository · gap-filler    config.runtime.toml[<wt-path>]     what pop recorded here
//	repository · gap-filler    config.runtime.toml[<trunk-path>]  inherited from the Trunk
//
// The key has no global home, so the global rung is absent rather than merely
// losing. The runtime rungs record what pop's own picker happened to pick, which
// makes them gap-fillers under every declaration of the same scope — not a
// checkout scope of their own (ADR-0212 decision 3 admits only two scopes).
//
// The inherited anchor is dropped when it is this very checkout, and the trunk
// runtime rung when the trunk is absent or is this checkout — the read-once
// guard, so a stale name is never double-warned by re-reading the same anchor.
func (e *repoScopeEnumerator) preferredSources() []preferredSource {
	sources := []preferredSource{
		{name: e.override.PreferredWorkbench},  // [repo."<path>"]
		{name: e.popPreferred(e.checkoutPath)}, // this worktree's .pop/config.toml
	}

	if anchor := e.inheritedAnchor(); canonicalPath(e.d, anchor) != e.canon {
		sources = append(sources, preferredSource{name: e.popPreferred(anchor)}) // inherited .pop/config.toml
	}

	sources = append(sources, preferredSource{ // what pop recorded for this worktree
		runtime:     true,
		runtimePath: e.checkoutPath,
		debugLabel:  "runtime preferred workbench",
	})

	if e.d.Trunk != nil {
		if trunkPath, ok := e.d.Trunk(e.checkoutPath); ok && trunkPath != "" &&
			canonicalPath(e.d, trunkPath) != e.canon {
			sources = append(sources, preferredSource{ // what pop recorded for the Trunk
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
