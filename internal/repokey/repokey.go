// Package repokey reads and writes the one directory component pop derives from
// a repository identity: "<basename>-<shortHash>".
//
// Two trees are named with it — per-repository Task storage
// (<data>/repos/<repoKey>) and the managed-worktree root
// (<data>/work/worktrees/<repoKey>/<worktree>). Because the basename travels in
// the path, a reader holding such a path recovers the repository name from the
// layout alone: no store to open and no git to fork. That is what lets a managed
// worktree keep its <repo>/ prefix when git cannot answer for it.
package repokey

import "strings"

// ShortHashLen is the number of hex characters of a repository identity hash
// retained in a repo key.
const ShortHashLen = 12

// Key joins a repository basename and its short identity hash into a repo key.
func Key(basename, shortHash string) string {
	return basename + "-" + shortHash
}

// Basename recovers the repository basename from a repo key. A name without a
// trailing "-<ShortHashLen hex chars>" is returned unchanged, so a basename that
// itself contains dashes survives and a directory that merely resembles a key is
// left alone.
func Basename(repoKey string) string {
	i := strings.LastIndex(repoKey, "-")
	if i < 0 {
		return repoKey
	}
	suffix := repoKey[i+1:]
	if len(suffix) != ShortHashLen || !isHex(suffix) {
		return repoKey
	}
	return repoKey[:i]
}

// HasKeyShape reports whether name is a repo key rather than a plain directory
// name — i.e. whether a basename can be recovered from it.
func HasKeyShape(name string) bool {
	return Basename(name) != name
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
