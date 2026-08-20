package conventions

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// The Project convention is rank 0 of the stack: the human's own answer for one
// project, held outside the repository so it needs no commit and no agreement
// from a team (ADR-0226 decision 4). This file is both its path derivation and
// its write side.
//
// Alone among pop's per-repository state it is keyed by the git remote rather
// than by Repository identity. The subject differs: a store, a binding and a
// config override are about *this machine's checkout*, for which a moved
// repository genuinely is a new subject, while a convention document is about
// the project as a thing that outlives any one clone. A remote is also derivable
// with no stored state, which a human-chosen name would not be.

// ErrEmptyConvention refuses a write with nothing in it. A file holding only
// whitespace reads as an absent layer to Resolve, so accepting one would leave
// the human believing they had stated something pop will never print.
var ErrEmptyConvention = errors.New("refusing to write an empty convention body")

// projectPathIn is one kind's document inside a project's documents directory.
// The write side reaches it through ProjectPath and the stack builds it from
// roots it already holds; both spell it here so they cannot drift.
func projectPathIn(projectDocs string, kind Kind) string {
	return filepath.Join(projectDocs, string(kind)+".md")
}

// ProjectPath returns where the human's document for kind in the project owning
// cwd lives.
func ProjectPath(d *Deps, kind Kind, cwd string) (string, error) {
	home, err := d.fs().UserHomeDir()
	if err != nil {
		return "", err
	}
	dir, err := projectDocsDir(d, filepath.Join(home, ".agents", "docs"), cwd)
	if err != nil {
		return "", err
	}
	return projectPathIn(dir, kind), nil
}

// projectDocsDir is the directory holding the human's documents for the project
// owning cwd: ~/.agents/docs/projects/<key>/. The key is the remote's slug where
// there is a remote, so every clone of one project reads one directory, and the
// Repository identity storage name where there is none — a repository nobody has
// published is knowable only as this machine's checkout of it.
func projectDocsDir(d *Deps, agentsDocs, cwd string) (string, error) {
	key, err := projectKey(d, cwd)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentsDocs, "projects", key), nil
}

// projectKey names the project a directory of documents belongs to. Asking git
// for the remote is the whole derivation in the common case; the fallback pays
// the extra git question only where the answer was not there.
func projectKey(d *Deps, cwd string) (string, error) {
	remote, err := d.tasksDeps().Git.CommandInDir(cwd, "remote", "get-url", "origin")
	if err == nil {
		if slug := remoteSlug(remote); slug != "" {
			return slug, nil
		}
	}
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), cwd)
	if err != nil {
		return "", err
	}
	// The storage directory's own name, rather than a second spelling of
	// basename-and-hash, so the fallback key cannot drift from the identity it
	// is named for.
	return filepath.Base(id.StorageDir), nil
}

// remoteSlug turns a remote URL into one path segment:
// git@github.com:tripledot/github_dashboard.git and
// https://github.com/tripledot/github_dashboard both become
// github.com-tripledot-github_dashboard. The scheme, the login and the .git
// suffix are the parts that differ between two URLs for one project, so they
// come off first and what is left collapses on its separators. A URL that
// reduces to nothing usable is no key, and the caller falls back.
func remoteSlug(remote string) string {
	s := strings.TrimSpace(remote)
	if _, after, ok := strings.Cut(s, "://"); ok {
		s = after
	}
	if _, after, ok := strings.Cut(s, "@"); ok {
		s = after
	}
	s = strings.TrimSuffix(s, ".git")
	var b strings.Builder
	dashed := false
	for _, r := range s {
		// Dots and underscores belong to a host or a repository name rather than
		// separating one from the other, so they survive; every other punctuation
		// run is a separator and becomes one dash.
		if r == '.' || r == '_' || r == '-' || isSlugAlphanumeric(r) {
			b.WriteRune(r)
			dashed = false
			continue
		}
		if !dashed {
			b.WriteRune('-')
			dashed = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	// A slug of nothing but dots would address a directory other than its own.
	if strings.Trim(slug, ".") == "" {
		return ""
	}
	return slug
}

func isSlugAlphanumeric(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// Set writes the human's document for kind in the project owning cwd and
// reports where it landed. It carries no frontmatter: every writable rank is the
// human's own statement, whose origin is not in question (ADR-0226 decision 5).
func Set(d *Deps, w io.Writer, kind Kind, cwd, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return ErrEmptyConvention
	}

	path, err := ProjectPath(d, kind, cwd)
	if err != nil {
		return err
	}
	if err := d.fs().MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	// Whether this replaces something matters to the writer: somebody who meant
	// to state a convention and instead overwrote what they wrote last month
	// should be told so by the same line that confirms the write.
	_, statErr := d.fs().Stat(path)
	replaced := statErr == nil

	if err := d.fs().WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
		return err
	}
	return RenderSet(w, kind, path, replaced)
}

// Unset removes the human's document for kind in this project and prints the
// stack that remains. The report is not a courtesy: removing the top rank hands
// the kind to whatever sat under it, and printing that on the spot is what stops
// `unset` being read as silencing the kind.
func Unset(d *Deps, w io.Writer, kind Kind, cwd string) error {
	path, err := ProjectPath(d, kind, cwd)
	if err != nil {
		return err
	}

	// Nothing to remove is a fact about the stack, not a failure: the caller
	// asked for a state, and that state already holds.
	removed := true
	if _, err := d.fs().Stat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed = false
	} else if err := d.fs().RemoveAll(path); err != nil {
		return err
	}

	remaining, err := Resolve(d, kind, cwd)
	if err != nil {
		return err
	}
	return RenderUnset(w, kind, path, removed, remaining)
}
