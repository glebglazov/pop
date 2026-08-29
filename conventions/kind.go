package conventions

import (
	"fmt"
	"strings"
)

// Kind is one member of the closed set of things pop can hold a Repo
// convention for. It is closed rather than free-form because each kind carries
// a Shipped convention — pop's own answer, written for that kind — so a kind
// pop has never heard of has nothing to answer with and nowhere sensible to
// look (ADR-0211, ADR-0226).
type Kind string

const (
	// KindRefine is the standard a Refiner holds a changeset against: what good
	// code looks like in this repository, as prose. Where the repository has
	// stated none, pop's shipped answer is a smell baseline that the
	// repository's own documents, linters and idiom override (ADR-0214,
	// ADR-0226 decision 2, ADR-0240).
	KindRefine Kind = "refine"
	// KindCommits is the commit-message grammar a repository's team writes
	// history in — types, scopes, subject style and body style, which are one
	// document and one git log sample rather than two kinds.
	KindCommits Kind = "commits"
	// KindIssueTracker is which Work store a repository files issues in and how
	// a planning skill publishes into it.
	KindIssueTracker Kind = "issue-tracker"
	// KindVerification is how work is checked in a repository: the build and
	// test invocation, which gate is whole-tree and which is scoped, and what
	// counts as evidence that one was run. It is a fact about a repository's
	// toolchain that no other pop surface holds, and it is the standard Agent
	// verification itself follows — the shared word is deliberate (ADR-0227
	// decision 4).
	KindVerification Kind = "verification"
)

// Shape is how a kind's answer is built into a prompt. It is a property of the
// kind rather than a habit of each call site, so a consumer cannot decide it
// locally and the author of the next kind owes it a decision (ADR-0227 decision
// 1). It says nothing about delivery: a kind reaches an agent by three paths and
// pop hands the prose over on only two of them — the body of a role-driving
// prompt and the `commits` block on a task set's manifest — the third being the
// agent running `pop conventions get` itself (ADR-0230).
type Shape string

const (
	// ShapeRoleDriving means the convention is an agent's entire mandate, so it
	// is the prompt body and pop supplies only a frame around it: a Role
	// preamble before it and a Response contract after it. There is then one
	// voice on what to check, rather than the team's document arguing with
	// pop's prompt (ADR-0227 decisions 2 and 3).
	ShapeRoleDriving Shape = "role-driving"
	// ShapeStepInforming means the convention is a fact a prompt about
	// something else needs: where a prompt carries it, it is a labelled block
	// inside a prompt pop wrote end to end, having no output contract of its
	// own to protect. It does not follow that a prompt carries it at all —
	// `issue-tracker` is read by an agent running the command.
	ShapeStepInforming Shape = "step-informing"
)

// Shape answers how this kind reaches an agent.
func (k Kind) Shape() Shape {
	switch k {
	case KindRefine, KindVerification:
		return ShapeRoleDriving
	case KindCommits, KindIssueTracker:
		return ShapeStepInforming
	}
	return ""
}

// Kinds returns every Convention kind in rank-independent declaration order:
// the order `get` with no kind walks, and the order an unknown kind is refused
// with.
func Kinds() []Kind {
	return []Kind{KindRefine, KindCommits, KindIssueTracker, KindVerification}
}

// Desc is the one-line description of what a kind answers, for a surface that
// lists kinds beside things of another sort and has to say what each one is. It
// is the counterpart of a config key's schema description, and it is here rather
// than in that surface so two surfaces cannot describe a kind differently.
func (k Kind) Desc() string {
	switch k {
	case KindRefine:
		return "What good code looks like in this repository — the standard a refine pass reads a changeset against."
	case KindCommits:
		return "How this repository writes commits — types, scopes, subject and body style."
	case KindIssueTracker:
		return "Which Work store this repository files issues in, and how a skill publishes into it."
	case KindVerification:
		return "How work is checked in this repository — the build and test gates, and what counts as having run them."
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
