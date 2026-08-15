package conventions

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// ErrNoConvention reports that every layer pop consulted was empty. It is a
// miss, not a failure: the caller has already been told where pop looked, and
// the CLI turns this into exit 1 rather than an error report.
var ErrNoConvention = errors.New("no convention found in any layer of the stack")

// Origin labels one rank of the Convention stack by who authored it. The four
// values are the stack, and the labels are what a reading agent quotes when it
// discloses which source it is following.
type Origin string

const (
	// OriginUserDefaults is ~/.agents/docs/<kind>.md — the human's preferences,
	// applying to every repository, which a team's document may overrule.
	OriginUserDefaults Origin = "user defaults"
	// OriginMemory is the Convention memory: the layer pop writes itself, filed
	// under Repository identity so it is per repository and per machine.
	OriginMemory Origin = "pop memory"
	// OriginRepository is docs/agents/<kind>.md — the team's rules, in version
	// control.
	OriginRepository Origin = "repository"
	// OriginOverlay is ~/.agents/docs/<kind>.overlay.md — the human's
	// constraints that must survive any repository, which is why the same author
	// holds both ends of the stack.
	OriginOverlay Origin = "user overlay"
)

// Scope is the one-phrase reminder of how far a layer reaches, printed beside
// its origin so the rank order reads as something other than four file paths.
func (o Origin) Scope() string {
	switch o {
	case OriginUserDefaults:
		return "yours, every repository"
	case OriginMemory:
		return "pop-written, this repository on this machine"
	case OriginRepository:
		return "the team's, in version control"
	case OriginOverlay:
		return "yours, overrides every repository"
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
// consulted, lowest first, present or not.
type Stack struct {
	Kind   Kind
	Layers []Layer
}

// Present returns the layers that hold something, in rank order.
func (s Stack) Present() []Layer {
	out := make([]Layer, 0, len(s.Layers))
	for _, l := range s.Layers {
		if l.Present {
			out = append(out, l)
		}
	}
	return out
}

// Empty reports a kind nothing anywhere has an answer for.
func (s Stack) Empty() bool { return len(s.Present()) == 0 }

// Top returns the highest-ranked layer that holds something — the one that wins
// a direct contradiction.
func (s Stack) Top() (Layer, bool) {
	present := s.Present()
	if len(present) == 0 {
		return Layer{}, false
	}
	return present[len(present)-1], true
}

// memory returns the Convention memory layer when it holds something.
func (s Stack) memory() (Layer, bool) {
	for _, l := range s.Layers {
		if l.Origin == OriginMemory && l.Present {
			return l, true
		}
	}
	return Layer{}, false
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
	return filepath.Join(id.StorageDir, "conventions", string(kind)+".md"), nil
}

// stackPaths derives the four layer paths for kind, lowest rank first, reading
// nothing. Both git-derived paths resolve from cwd, so a caller outside a
// repository is refused here rather than silently getting a two-layer stack.
func stackPaths(d *Deps, kind Kind, cwd string) ([]Layer, error) {
	home, err := d.fs().UserHomeDir()
	if err != nil {
		return nil, err
	}
	agentsDocs := filepath.Join(home, ".agents", "docs")

	memory, err := MemoryPath(d, kind, cwd)
	if err != nil {
		return nil, err
	}

	// The repository's document is read from the checkout the caller stands in,
	// not from the main worktree: it is version-controlled, so the copy in front
	// of you is the one your branch is working against.
	topLevel, err := tasks.NormalizeProjectPathWith(d.tasksDeps(), cwd)
	if err != nil {
		return nil, err
	}

	return []Layer{
		{Origin: OriginUserDefaults, Path: filepath.Join(agentsDocs, string(kind)+".md")},
		{Origin: OriginMemory, Path: memory},
		{Origin: OriginRepository, Path: filepath.Join(topLevel, "docs", "agents", string(kind)+".md")},
		{Origin: OriginOverlay, Path: filepath.Join(agentsDocs, string(kind)+".overlay.md")},
	}, nil
}

// Resolve reads the whole Convention stack for kind in the repository owning
// cwd. Every rank is returned whether or not it holds something: a caller that
// found nothing still needs to be told where pop looked.
func Resolve(d *Deps, kind Kind, cwd string) (Stack, error) {
	layers, err := stackPaths(d, kind, cwd)
	if err != nil {
		return Stack{}, err
	}
	for i := range layers {
		readLayer(d, &layers[i])
	}
	return Stack{Kind: kind, Layers: layers}, nil
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
