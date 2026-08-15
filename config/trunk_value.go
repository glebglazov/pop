package config

import (
	"fmt"
	"strings"
)

// TrunkPath is a repository's Trunk worktree stated as a config value: the path
// of the checkout every managed worktree forks from (ADR-0212 decision 3). A
// repository has one fork base, so the value belongs to the repository and the
// [repo] block carrying it only says which repository is meant — the block's key
// no longer has to be the trunk itself.
//
// It also reads the spelling that decision retired. `trunk = true` marked the
// checkout that keyed the block instead of naming one, and a boolean carries no
// path, so it decodes to a marker that Resolve turns back into the block's own
// key. An existing machine therefore keeps its trunk with no operator action, in
// the manner of the flat `.pop.toml` read-path fold (ADR-0137).
type TrunkPath string

// trunkIsBlockKey is the retired `trunk = true` held until a reader supplies the
// block key it meant. No path a human can write contains a NUL, so it can never
// collide with a real value, and it never leaves this type: Resolve is the only
// way to read a declaration.
const trunkIsBlockKey TrunkPath = "\x00block-key"

// UnmarshalTOML accepts either spelling. `trunk = false` said "this checkout is
// not the trunk", which under a path value is a block that names no trunk at
// all, so it reads as the empty path rather than as a declaration.
func (t *TrunkPath) UnmarshalTOML(v any) error {
	switch value := v.(type) {
	case string:
		*t = TrunkPath(strings.TrimSpace(value))
	case bool:
		if value {
			*t = trunkIsBlockKey
		}
	default:
		return fmt.Errorf("want the path of a checkout, got %T", v)
	}
	return nil
}

// Resolve returns the checkout path this declaration names, given the key of the
// [repo] block that carries it — which a path value ignores and a folded legacy
// boolean is entirely made of. The bool is false when nothing is declared here,
// so a nil receiver, an empty path and a block with no trunk key all read alike.
func (t *TrunkPath) Resolve(blockKey string) (string, bool) {
	if t == nil || *t == "" {
		return "", false
	}
	if *t == trunkIsBlockKey {
		blockKey = strings.TrimSpace(blockKey)
		return blockKey, blockKey != ""
	}
	return string(*t), true
}

// IsTrunk reports whether the repository's resolved Trunk worktree is this very
// checkout. Since decision 3 the resolved value answers for every worktree of the
// repository, so a caller asking "am I standing in the trunk" — the fork-free
// representative resolvers do, once per checkout they scanned — compares paths
// rather than reading a flag that was only ever set for one of them. Both sides
// are canonicalized, so a `~`-spelled declaration matches a real checkout.
func (c RepoConfig) IsTrunk(d *Deps, checkoutPath string) bool {
	if strings.TrimSpace(c.Trunk) == "" || strings.TrimSpace(checkoutPath) == "" {
		return false
	}
	if d == nil {
		d = defaultDeps
	}
	return canonicalPath(d, c.Trunk) == canonicalPath(d, checkoutPath)
}
