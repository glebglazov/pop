package routine

import (
	"io"
	"os"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the routine package.
type Deps struct {
	FS            deps.FileSystem
	OpenEditor    func(path string) error
	IsInteractive func() bool
	Now           func() time.Time
	Stdout        io.Writer
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		FS:            deps.NewRealFileSystem(),
		OpenEditor:    defaultOpenEditor,
		IsInteractive: defaultIsInteractive,
		Now:           time.Now,
		Stdout:        os.Stdout,
	}
}

var defaultDeps = DefaultDeps()
