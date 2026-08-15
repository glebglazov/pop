package conventions

import (
	"fmt"
	"strings"
)

// Kind is one member of the closed set of things pop can hold a Repo
// convention for. It is closed rather than free-form because each kind's
// Convention recipe is kind-specific work pop must know how to do, so a kind
// pop has never heard of has no method to offer and nowhere sensible to look
// (ADR-0211).
type Kind string

const (
	// KindCommits is the commit-message grammar a repository's team writes
	// history in — types, scopes, subject style and body style, which are one
	// document and one git log sample rather than two kinds.
	KindCommits Kind = "commits"
	// KindIssueTracker is which Work store a repository files issues in and how
	// a planning skill publishes into it.
	KindIssueTracker Kind = "issue-tracker"
)

// Kinds returns every Convention kind in rank-independent declaration order:
// the order `get` with no kind walks, and the order an unknown kind is refused
// with.
func Kinds() []Kind { return []Kind{KindCommits, KindIssueTracker} }

// KindNames returns the kinds as the strings a caller types.
func KindNames() []string {
	kinds := Kinds()
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, string(k))
	}
	return names
}

// UnknownKindError is the refusal for a kind pop does not hold conventions for,
// naming the ones that exist so the caller does not have to go looking. It
// mirrors config.UnknownRepoSettingError deliberately: the two surfaces refuse
// an unknown name the same way.
func UnknownKindError(kind string) error {
	return fmt.Errorf("unknown convention kind %q; known kinds: %s",
		kind, strings.Join(KindNames(), ", "))
}

// ParseKind resolves a typed name to a Kind, refusing anything outside the enum
// before any path is derived or any file is read.
func ParseKind(name string) (Kind, error) {
	for _, k := range Kinds() {
		if string(k) == name {
			return k, nil
		}
	}
	return "", UnknownKindError(name)
}
