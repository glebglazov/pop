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

// Desc is the one-line description of what a kind answers, for a surface that
// lists kinds beside things of another sort and has to say what each one is. It
// is the counterpart of a config key's schema description, and it is here rather
// than in that surface so two surfaces cannot describe a kind differently.
func (k Kind) Desc() string {
	switch k {
	case KindCommits:
		return "How this repository writes commits — types, scopes, subject and body style."
	case KindIssueTracker:
		return "Which Work store this repository files issues in, and how a skill publishes into it."
	}
	return ""
}

// rowKeyPrefix is how a kind is addressed where it sits beside config keys in
// one list. It is the command a reader would type to see the same stack in full,
// so the row's own text says where it came from (ADR-0212 decision 8).
const rowKeyPrefix = "conventions."

// RowKey spells one kind as a row of that list addresses it.
func RowKey(kind Kind) string { return rowKeyPrefix + string(kind) }

// RowKind reads a row key back as the kind it names, and false for anything
// else. It is how a writer holding one key tells a convention row from a config
// key before it decides what a write to it means.
func RowKind(key string) (Kind, bool) {
	name, ok := strings.CutPrefix(key, rowKeyPrefix)
	if !ok {
		return "", false
	}
	for _, k := range Kinds() {
		if string(k) == name {
			return k, true
		}
	}
	return "", false
}

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
