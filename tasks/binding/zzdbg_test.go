package binding

import (
	"io"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestDbgManaged2(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "bound-branch")
	td := lifecycleTestDeps(t)
	defPath := seedRegisteredSet(t, td, repo, "set-m")
	seedLifecycleBinding(t, td, repo, "set-m", Binding{RuntimePath: wt, Branch: "bound-branch", Project: "x"})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := BindWorktree(td, nil, cfg, "set-m", repo, BindWorktreeOptions{Managed: true, Force: true}, LifecycleHooks{}, io.Discard)
	t.Logf("bind err=%v", err)

	n := len(loadLifecycleBindings(t, td))
	t.Logf("bindings=%d", n)

	i1, err := tasks.RegisteredWorktreeIntent(td, defPath, "set-m")
	t.Logf("after intent=%+v err=%v", i1, err)
}
