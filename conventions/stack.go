package conventions

import (
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// Origin labels one rank of the Convention stack by who authored it. The five
// values are the stack, and the labels are what a reading agent quotes when it
// discloses which source it is following.
type Origin string

const (
	// OriginProject is ~/.agents/docs/projects/<slug>/<kind>.md — the human's own
	// answer for one project, and the first rank consulted. It outranks their
	// global document because the more specific of two documents by the same
	// author wins, which makes it the rank to reach for when overriding
	// everything for one project (ADR-0226 decision 4).
	OriginProject Origin = "user project"
	// OriginGlobal is ~/.agents/docs/<kind>.md — the human's own answer, applying
	// to every repository. It outranks the team's document because pop resolves
	// conventions on one human's machine, on that human's behalf, so where the
	// two disagree the person at the terminal is the one being served
	// (ADR-0223 decision 2).
	OriginGlobal Origin = "user global"
	// OriginRepository is docs/agents/<kind>.md — the team's rules, in version
	// control, and the answer wherever the human has written none of their own.
	OriginRepository Origin = "repository"
	// OriginShipped is pop's own answer for the kind: the last rank, embedded
	// rather than read from disk, and always holding something — which is what
	// makes a kind always resolve to rules somebody can follow. Any written rank
	// above it displaces it whole (ADR-0226 decision 1).
	OriginShipped Origin = "shipped"
	// OriginOverlay is ~/.agents/docs/<kind>.overlay.md — the human's
	// constraints, which ride along with whichever rank answered rather than
	// competing with it. It is the one layer that appends (ADR-0223 decision 3).
	OriginOverlay Origin = "user overlay"
)

// Scope is the one-phrase reminder of how far a layer reaches, printed beside
// its origin so a labelled block reads as something other than a file path.
func (o Origin) Scope() string {
	switch o {
	case OriginProject:
		return "yours, this project"
	case OriginGlobal:
		return "yours, every repository"
	case OriginRepository:
		return "the team's, in version control"
	case OriginShipped:
		return "pop's own, displaced by any above"
	case OriginOverlay:
		return "yours, appended to whichever answered"
	}
	return ""
}

// Layer is one rank of the Convention stack: where pop looked and what it found
// there. Every rank is one document and nothing else — no rank carries pop's
// bookkeeping, all the writable ones being the human's own statement
// (ADR-0226 decision 5).
type Layer struct {
	Origin  Origin
	Path    string
	Present bool
	// Body is the document's prose, trimmed. It is the whole of what a reader is
	// handed when this rank answers.
	Body string
}

// Stack is one Convention kind resolved for one repository: every rank pop
// consulted, present or not. It resolves to exactly one answer plus the
// overlay — the ranks below the answer are not consulted at all — and because
// the last rank is pop's own shipped answer, it always resolves to rules
// somebody can follow (ADR-0226).
type Stack struct {
	Kind   Kind
	Layers []Layer
}

// writtenRanks is the order the ranks somebody wrote resolve in: the human's
// document for this project, then their document for every repository, then the
// team's. The first that holds something is the answer, and the rest are not
// read into it — nothing composes. Beneath all three sits the shipped rank,
// which nobody wrote and which is reached by falling off the end of this list.
var writtenRanks = []Origin{OriginProject, OriginGlobal, OriginRepository}

// speaks returns one rank when it holds something.
func (s Stack) speaks(origin Origin) (Layer, bool) {
	for _, l := range s.Layers {
		if l.Origin == origin && l.Present {
			return l, true
		}
	}
	return Layer{}, false
}

// Answer returns the one layer in force for this kind. A kind nobody has
// written an answer to answers with pop's own, so there is no miss for a caller
// to handle and no case in which the reader is handed anything other than rules
// to follow (ADR-0226 decision 1).
func (s Stack) Answer() Layer {
	for _, origin := range writtenRanks {
		if l, ok := s.speaks(origin); ok {
			return l
		}
	}
	return shippedLayer(s.Kind)
}

// Overlay returns the human's overlay when it holds something. It is appended
// to whichever rank answered, so it is never in competition with one.
func (s Stack) Overlay() (Layer, bool) { return s.speaks(OriginOverlay) }

// Contested reports that a rank below the answer also holds something, so a
// document somebody wrote is quietly losing. Under winner-take-all that is a real
// state a reader wants surfaced; the overlay is never a contender, since it
// appends rather than displaces (ADR-0223).
func (s Stack) Contested() bool {
	speaking := 0
	for _, origin := range writtenRanks {
		if _, ok := s.speaks(origin); ok {
			speaking++
		}
	}
	return speaking > 1
}

// stackRoots is everything a stack's paths derive from that is about the
// repository rather than about the kind: the human's documents directory, the
// directory holding their documents for this project, and the checkout's top
// level. Resolving it costs git questions, so it is resolved once and every
// kind's paths are built from it — a caller asking for all the kinds at once
// pays for one repository, not one per kind.
type stackRoots struct {
	agentsDocs  string
	projectDocs string
	topLevel    string
}

// resolveStackRoots answers those three for the repository owning cwd. Both
// repository-derived roots resolve from cwd, so a caller outside a repository is
// refused here — by the project rank, which has neither a remote nor a
// Repository identity to key itself by — rather than silently getting a stack
// with no repository in it.
func resolveStackRoots(d *Deps, cwd string) (stackRoots, error) {
	home, err := d.fs().UserHomeDir()
	if err != nil {
		return stackRoots{}, err
	}
	agentsDocs := filepath.Join(home, ".agents", "docs")
	projectDocs, err := projectDocsDir(d, agentsDocs, cwd)
	if err != nil {
		return stackRoots{}, err
	}
	// The repository's document is read from the checkout the caller stands in,
	// not from the main worktree: it is version-controlled, so the copy in front
	// of you is the one your branch is working against.
	topLevel, err := tasks.NormalizeProjectPathWith(d.tasksDeps(), cwd)
	if err != nil {
		return stackRoots{}, err
	}
	return stackRoots{
		agentsDocs:  agentsDocs,
		projectDocs: projectDocs,
		topLevel:    topLevel,
	}, nil
}

// layers derives kind's ranks in resolution order, reading no file. The three
// written ranks come first, best first, then pop's own answer for when none of
// them does, and the overlay last because it is appended rather than ranked.
func (r stackRoots) layers(kind Kind) []Layer {
	return []Layer{
		{Origin: OriginProject, Path: projectPathIn(r.projectDocs, kind)},
		{Origin: OriginGlobal, Path: filepath.Join(r.agentsDocs, string(kind)+".md")},
		{Origin: OriginRepository, Path: filepath.Join(r.topLevel, "docs", "agents", string(kind)+".md")},
		shippedLayer(kind),
		{Origin: OriginOverlay, Path: overlayPathIn(r.agentsDocs, kind)},
	}
}

// Resolve reads every rank of the Convention stack for kind in the repository
// owning cwd. They all come back whether or not they hold something: a caller
// still needs to know which layer an edit would land in, and which ranks the
// answer stood down.
func Resolve(d *Deps, kind Kind, cwd string) (Stack, error) {
	stacks, err := ResolveAll(d, cwd, kind)
	if err != nil {
		return Stack{}, err
	}
	return stacks[0], nil
}

// ResolveAll reads several kinds' stacks in one pass over one repository, in the
// order asked for. Every caller that shows more than one kind — `get` with no
// kind, the Config dashboard's rows — goes through here, because the repository
// each stack is about is the same one.
//
// Every rank is read even though at most two are shown: which one won is only
// knowable by looking, and Contested is a fact about the ones that lost.
func ResolveAll(d *Deps, cwd string, kinds ...Kind) ([]Stack, error) {
	roots, err := resolveStackRoots(d, cwd)
	if err != nil {
		return nil, err
	}
	stacks := make([]Stack, 0, len(kinds))
	for _, kind := range kinds {
		layers := roots.layers(kind)
		for i := range layers {
			readLayer(d, &layers[i])
		}
		stacks = append(stacks, Stack{Kind: kind, Layers: layers})
	}
	return stacks, nil
}

// readLayer fills in what is on disk at a layer's path. An unreadable path is
// an absent layer, not an error: the written ranks are expected to be missing
// most of the time, and a stack that refused on the first missing file could
// never resolve. The shipped rank is embedded and already holds its body, so
// there is nothing to read for it.
func readLayer(d *Deps, l *Layer) {
	if l.Origin == OriginShipped {
		return
	}
	raw, err := d.fs().ReadFile(l.Path)
	if err != nil {
		return
	}
	body := strings.TrimSpace(string(raw))
	// A file holding only whitespace says nothing, and printing it as a layer
	// would put an empty labelled section in front of the reader.
	if body == "" {
		return
	}
	l.Present = true
	l.Body = body
}
