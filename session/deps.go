package session

import (
	"os"

	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for session operations.
type Deps struct {
	Tmux   deps.Tmux
	InTmux func() bool
}

// DefaultDeps returns dependencies using real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		Tmux:   deps.NewRealTmux(),
		InTmux: func() bool { return os.Getenv("TMUX") != "" },
	}
}
