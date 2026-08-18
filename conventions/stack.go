package conventions

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// ErrNoConvention reports that no rank pop consulted holds an answer and no
// overlay exists either. It is a miss, not a failure: the caller has already
// been told where pop looked, and the CLI turns this into exit 1 rather than an
// error report.
var ErrNoConvention = errors.New("no convention found in any layer of the stack")

// Origin labels one rank of the Convention stack by who authored it. The four
// values are the stack, and the labels are what a reading agent quotes when it
// discloses which source it is following.
type Origin string

const (
	// OriginUserDefaults is ~/.agents/docs/<kind>.md — the human's own answer,
	// applying to every repository. It is the first rank consulted: pop resolves
	// conventions on one human's machine, on that human's behalf, so where the
	// two disagree the person at the terminal is the one being served
	// (ADR-0223 decision 2).
	OriginUserDefaults Origin = "user defaults"
	// OriginRepository is docs/agents/<kind>.md — the team's rules, in version
	// control, and the answer wherever the human has written none of their own.
	OriginRepository Origin = "repository"
	// OriginMemory is the Convention memory: the layer pop writes itself, filed
	// under Repository identity so it is per repository and per machine. It is
	// the last rank, and it stands down as soon as either document exists — it
	// is pop's stand-in for a written answer, and a written answer makes it
	// redundant (ADR-0223 decision 4).
	OriginMemory Origin = "pop memory"
	// OriginOverlay is ~/.agents/docs/<kind>.overlay.md — the human's
	// constraints, which ride along with whichever rank answered rather than
	// competing with it. It is the one layer that appends (ADR-0223 decision 3).
	OriginOverlay Origin = "user overlay"
)

// Scope is the one-phrase reminder of how far a layer reaches, printed beside
// its origin so a labelled block reads as something other than a file path.
func (o Origin) Scope() string {
	switch o {
	case OriginUserDefaults:
		return "yours, every repository"
	case OriginRepository:
		return "the team's, in version control"
	case OriginMemory:
		return "pop-written, this repository on this machine"
	case OriginOverlay:
		return "yours, appended to any answer"
	}
	return ""
}

// Layer is one rank of the Convention stack: where pop looked, what it found
// there, and — for Convention memory — the frontmatter recording how it got
// there.
type Layer struct {
	Origin  Origin
	Path    string
	Present bool
	// Body is the layer's prose with any frontmatter removed, so the reader is
	// handed the convention and not pop's bookkeeping.
	Body string
	// DerivedFrom and DerivedAt are Convention memory's provenance. They are the
	// only reason the frontmatter exists: the disclosure line quotes them so
	// "which source am I using" cannot drift between the skills that ask.
	DerivedFrom string
	DerivedAt   string
}

// Stack is one Convention kind resolved for one repository: every rank pop
// consulted, present or not. It resolves to exactly one answer plus the
// overlay — the ranks below the answer are not consulted at all (ADR-0223).
type Stack struct {
	Kind   Kind
	Layers []Layer
}

// answerRanks is the order a kind resolves in: the human's own document, then
// the team's, then pop's memory. The first that holds something is the answer,
// and the rest are not read into it — nothing composes.
var answerRanks = []Origin{OriginUserDefaults, OriginRepository, OriginMemory}

// speaks returns one rank when it holds something.
func (s Stack) speaks(origin Origin) (Layer, bool) {
	for _, l := range s.Layers {
		if l.Origin == origin && l.Present {
			return l, true
		}
	}
	return Layer{}, false
}

// Answer returns the one layer in force for this kind, and false for a kind
// nothing has written an answer to.
func (s Stack) Answer() (Layer, bool) {
	for _, origin := range answerRanks {
		if l, ok := s.speaks(origin); ok {
			return l, true
		}
	}
	return Layer{}, false
}

// Overlay returns the human's overlay when it holds something. It is appended
// to whichever rank answered, so it is never in competition with one.
func (s Stack) Overlay() (Layer, bool) { return s.speaks(OriginOverlay) }

// Empty reports a kind neither an answer nor an overlay exists for.
func (s Stack) Empty() bool {
	if _, ok := s.Answer(); ok {
		return false
	}
	_, ok := s.Overlay()
	return !ok
}

// Contested reports that a rank below the answer also holds something, so a
// document or a memory is quietly losing. Under winner-take-all that is a real
// state a reader wants surfaced; the overlay is never a contender, since it
// appends rather than displaces (ADR-0223).
func (s Stack) Contested() bool {
	speaking := 0
	for _, origin := range answerRanks {
		if _, ok := s.speaks(origin); ok {
			speaking++
		}
	}
	return speaking > 1
}

// MemoryPath returns where Convention memory for kind lives: one Markdown file
// per kind under the repository's Task storage directory, beside tasks/ and
// maps/. It is derived from Repository identity rather than from the checkout,
// which is what makes two worktrees of one repository read one file.
func MemoryPath(d *Deps, kind Kind, cwd string) (string, error) {
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), cwd)
	if err != nil {
		return "", err
	}
	return memoryPathIn(id.StorageDir, kind), nil
}

// memoryPathIn is where Convention memory sits under a repository's storage
// directory. The write side reaches it through MemoryPath and the stack builds
// it from roots it already holds; both spell it here so they cannot drift.
func memoryPathIn(storageDir string, kind Kind) string {
	return filepath.Join(storageDir, "conventions", string(kind)+".md")
}

// stackRoots is everything a stack's paths derive from that is about the
// repository rather than about the kind: the human's documents directory, the
// repository's Task storage directory and the checkout's top level. Resolving
// it costs two git questions, so it is resolved once and every kind's paths are
// built from it — a caller asking for all the kinds at once pays for one
// repository, not one per kind.
type stackRoots struct {
	agentsDocs string
	storageDir string
	topLevel   string
}

// resolveStackRoots answers those three for the repository owning cwd. Both
// git-derived roots resolve from cwd, so a caller outside a repository is
// refused here rather than silently getting a two-layer stack.
func resolveStackRoots(d *Deps, cwd string) (stackRoots, error) {
	home, err := d.fs().UserHomeDir()
	if err != nil {
		return stackRoots{}, err
	}
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), cwd)
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
		agentsDocs: filepath.Join(home, ".agents", "docs"),
		storageDir: id.StorageDir,
		topLevel:   topLevel,
	}, nil
}

// layers derives the four layer paths for kind, in resolution order, reading
// nothing. The three answer ranks come first, best first, and the overlay last
// because it is appended rather than ranked.
func (r stackRoots) layers(kind Kind) []Layer {
	return []Layer{
		{Origin: OriginUserDefaults, Path: filepath.Join(r.agentsDocs, string(kind)+".md")},
		{Origin: OriginRepository, Path: filepath.Join(r.topLevel, "docs", "agents", string(kind)+".md")},
		{Origin: OriginMemory, Path: memoryPathIn(r.storageDir, kind)},
		{Origin: OriginOverlay, Path: overlayPathIn(r.agentsDocs, kind)},
	}
}

// Resolve reads every rank of the Convention stack for kind in the repository
// owning cwd. All four come back whether or not they hold something: a caller
// that found nothing still needs to be told where pop looked, and one that did
// still needs to know which layer an edit would land in.
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
// an absent layer, not an error: three of the four are expected to be missing
// most of the time, and a stack that refused on the first missing file could
// never resolve.
func readLayer(d *Deps, l *Layer) {
	raw, err := d.fs().ReadFile(l.Path)
	if err != nil {
		return
	}
	body := string(raw)
	if l.Origin == OriginMemory {
		var front map[string]string
		front, body = splitFrontmatter(body)
		l.DerivedFrom = front["derived_from"]
		l.DerivedAt = front["derived_at"]
	}
	body = strings.TrimSpace(body)
	// A file holding only whitespace (or only frontmatter) says nothing, and
	// printing it as a layer would put an empty labelled section in front of the
	// reader.
	if body == "" {
		return
	}
	l.Present = true
	l.Body = body
}

// splitFrontmatter peels a leading `---` fenced block of `key: value` lines off
// a document, returning its fields and the prose beneath. Convention memory is
// the only layer that carries one, and it carries one only to record what the
// convention was derived from.
func splitFrontmatter(body string) (map[string]string, string) {
	fields := map[string]string{}
	rest := strings.TrimLeft(body, "\r\n")
	if !strings.HasPrefix(rest, "---") {
		return fields, body
	}
	lines := strings.Split(rest, "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return fields, body
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return fields, strings.Join(lines[i+1:], "\n")
		}
		key, value, ok := strings.Cut(lines[i], ":")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	// An unterminated fence is not frontmatter; hand the document back whole
	// rather than swallowing it.
	return map[string]string{}, body
}
