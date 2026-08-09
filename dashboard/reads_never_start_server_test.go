package dashboard

import (
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks/drain"
)

// TestReadSurfacesNeverStartAServer covers ADR-0199 decision 8 against the
// Work dashboard / status / history read paths: with no live sessions arranged
// on the fake, those surfaces report an empty world and leave Fake.Live empty
// — they must not reach Ensure.
func TestReadSurfacesNeverStartAServer(t *testing.T) {
	f := &tmuxtest.Fake{}
	d := dashboardTestDeps(t, nil, nil)
	d.Tmux = f

	_ = loadLivePaneCache(d)
	_ = history.TmuxSessionActivityWith(&history.Deps{Tmux: f, Tasks: d.Tasks})

	if _, err := drain.BuildStatus(d, &config.Config{}); err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}

	if len(f.Live) != 0 {
		t.Fatalf("Live = %v after read surfaces, want empty (no server started)", f.Live)
	}
}
