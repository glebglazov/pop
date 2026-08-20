package conventions

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// One writer serves every writable rank (ADR-0226 decision 6), so these tests
// drive it by rank: the two ranks that need no repository round-trip here, and
// the project rank — which is keyed by a git remote — round-trips through the
// command tests instead.

func writerDeps(t *testing.T) (*Deps, string) {
	t.Helper()
	home := t.TempDir()
	real := deps.NewRealFileSystem()
	return &Deps{FS: &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		ReadFileFunc:    real.ReadFile,
		WriteFileFunc:   real.WriteFile,
		MkdirAllFunc:    real.MkdirAll,
		StatFunc:        real.Stat,
		RemoveAllFunc:   real.RemoveAll,
	}}, home
}

// The rank named is the file written, and it is the same file the stack reads
// back — a writer that landed a rank away from the reader would be the whole bug
// the rank argument exists to close.
func TestWriteLandsAtTheRankItWasGiven(t *testing.T) {
	d, home := writerDeps(t)
	docs := filepath.Join(home, ".agents", "docs")

	for _, tc := range []struct {
		origin Origin
		want   string
	}{
		{OriginGlobal, filepath.Join(docs, "commits.md")},
		{OriginOverlay, filepath.Join(docs, "commits.overlay.md")},
	} {
		path, replaced, err := Write(d, tc.origin, KindCommits, "", "\nNever mention pop in a subject.\n")
		if err != nil {
			t.Fatalf("Write(%s) error: %v", tc.origin, err)
		}
		if path != tc.want || replaced {
			t.Fatalf("Write(%s) = %q, replaced=%v, want %q and a first write", tc.origin, path, replaced, tc.want)
		}
		// The file is the human's own statement, so it holds their prose and none
		// of pop's bookkeeping (ADR-0226 decision 5).
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", tc.origin, err)
		}
		if strings.TrimSpace(string(raw)) != "Never mention pop in a subject." || strings.HasPrefix(string(raw), "---") {
			t.Errorf("%s holds %q, want the trimmed body and no frontmatter", tc.origin, raw)
		}
		if _, replaced, err := Write(d, tc.origin, KindCommits, "", "Second reading."); err != nil || !replaced {
			t.Errorf("second Write(%s) = replaced %v, %v, want a reported replacement", tc.origin, replaced, err)
		}
	}

	// The overlay reader opens exactly what the writer wrote.
	layer, err := Overlay(d, KindCommits)
	if err != nil || !layer.Present || layer.Body != "Second reading." {
		t.Fatalf("Overlay() = %+v, %v, want the written prose", layer, err)
	}
}

// Nothing to remove is the state the caller asked for, already holding.
func TestEraseRemovesOnceAndThenChangesNothing(t *testing.T) {
	d, _ := writerDeps(t)
	if _, _, err := Write(d, OriginOverlay, KindCommits, "", "Never mention pop."); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	path, removed, err := Erase(d, OriginOverlay, KindCommits, "")
	if err != nil || !removed {
		t.Fatalf("Erase() = %v, %v, want a removal", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the document survived Erase at %s", path)
	}
	if _, removed, err := Erase(d, OriginOverlay, KindCommits, ""); err != nil || removed {
		t.Errorf("Erase() on nothing = %v, %v, want no failure and no removal", removed, err)
	}
}

// A body that reads as an absent layer is refused at every rank, because every
// rank is read by the same reader: accepting one would leave the human believing
// they had stated something pop will never print.
func TestWriteRefusesAnEmptyBodyAtEveryRank(t *testing.T) {
	d, home := writerDeps(t)

	for _, origin := range WritableRanks {
		if _, _, err := Write(d, origin, KindCommits, "", "  \n\t\n"); !errors.Is(err, ErrEmptyConvention) {
			t.Errorf("Write(%s) = %v, want ErrEmptyConvention", origin, err)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(home, ".agents", "docs")); err == nil && len(entries) > 0 {
		t.Errorf("a refused body still produced files: %v", entries)
	}
}

func TestParseRankAcceptsEveryWritableRank(t *testing.T) {
	for _, origin := range WritableRanks {
		got, err := ParseRank(origin.RankName(), KindCommits)
		if err != nil || got != origin {
			t.Errorf("ParseRank(%q) = %q, %v, want %q", origin.RankName(), got, err, origin)
		}
	}
}

// Every way of not naming a writable rank is refused with what the reader needs
// to name one, and the repository rank is refused with its path and the reason
// rather than as a word pop does not know.
func TestParseRankRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		named string
		want  []string
	}{
		{"nothing named", "", []string{"name the rank"}},
		{"the team's document", "repository", []string{filepath.Join("docs", "agents", "commits.md"), "version control", "diff"}},
		{"pop's own answer", "shipped", []string{"built into the binary", "conventions default commits"}},
		{"not a rank at all", "memory", []string{`"memory"`, "no rank"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRank(tc.named, KindCommits)
			if err == nil {
				t.Fatalf("ParseRank(%q) = %q, want a refusal", tc.named, got)
			}
			// Every refusal hands the reader the ranks they can write, with what
			// each one reaches — the choice, not a list of words to look up.
			for _, origin := range WritableRanks {
				for _, part := range []string{"--" + origin.RankName(), origin.Scope()} {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("refusal %q does not offer %q", err, part)
					}
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not say %q", err, want)
				}
			}
		})
	}
}

// A rank nobody can write has no path to write to, so the writer refuses before
// deriving one — the argument is a rank, and not every rank is a destination.
func TestWriteRefusesARankNobodyWrites(t *testing.T) {
	d, _ := writerDeps(t)
	for _, origin := range []Origin{OriginRepository, OriginShipped} {
		if _, _, err := Write(d, origin, KindCommits, "", "Anything."); err == nil {
			t.Errorf("Write(%s) was accepted, want a refusal", origin)
		}
	}
}

// TestOneWriterServesEveryRank guards the collapse (ADR-0226 decision 6): if a
// second function in this package touched a convention file, the rank a write
// landed at would again be implicit in which function the caller reached for.
func TestOneWriterServesEveryRank(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if filepath.Base(name) == "write.go" {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "WriteFile", "RemoveAll":
					t.Errorf("%s calls %s; every convention write goes through write.go, which takes the rank as an argument",
						filepath.Base(name), sel.Sel.Name)
				}
				return true
			})
		}
	}
}
