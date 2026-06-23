package config

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

func TestProbeGlob(t *testing.T) {
	for _, pat := range []string{"[a-/*", "*", "foo/[/*", "**"} {
		base, p := doublestar.SplitPattern("/tmp/" + pat)
		_, err := doublestar.Glob(fakeFS{}, p, doublestar.WithNoHidden())
		fmt.Printf("pat=%q base=%q p=%q err=%v\n", pat, base, p, err)
	}
}

type fakeFS struct{}

func (fakeFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }
