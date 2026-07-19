package queue

// Throwaway repro for the `av` dashboard verify-verb investigation. Delete me.

import (
	"fmt"
	"os"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func TestReproAVVerify(t *testing.T) {
	if os.Getenv("REPRO_AV") == "" {
		t.Skip("repro only")
	}
	if dir := os.Getenv("REPRO_DATA"); dir != "" {
		t.Setenv("XDG_DATA_HOME", dir)
	}
	d := DefaultDeps()
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	fmt.Printf("projects: %d\n", len(projects))
	for _, p := range projects {
		fmt.Printf("  project %s -> %s\n", p.Name, p.Path)
	}
	statics, err := dashboardRepoStatics(d, cfg, projects)
	if err != nil {
		t.Fatalf("statics: %v", err)
	}
	fmt.Printf("statics: %d\n", len(statics))
	var target *DashboardRow
	for _, st := range statics {
		refresh, rerr := d.refresh(st.defPath)
		nrows := -1
		if refresh != nil {
			nrows = len(refresh.Rows)
		}
		fmt.Printf("STATIC def=%s repoKey=%s refreshRows=%d refreshErr=%v\n", st.defPath, st.repoKey, nrows, rerr)
		rows, err := dashboardRowsForStatic(d, cfg, st)
		if err != nil {
			fmt.Printf("ROWS ERR %s: %v\n", st.defPath, err)
			continue
		}
		for i := range rows {
			r := rows[i]
			fmt.Printf("ROW %-22s %-45s status=%-15s live=%v parked=%v eligible=%v runtime=%s\n",
				r.Project, r.SetID, r.RawStatus, r.LiveDrain, r.Parked, dashboardVerifyEligible(r), r.RuntimePath)
			if r.SetID == os.Getenv("REPRO_AV") {
				target = &rows[i]
			}
		}
	}
	if target == nil {
		fmt.Println("no target row matched REPRO_AV; done")
		return
	}
	mock := &deps.MockTmux{
		HasSessionFunc: func(name string) bool {
			fmt.Printf("TMUX HasSession(%q)\n", name)
			out, err := d.Tmux.Command("has-session", "-t", name)
			fmt.Printf("  real has-session -> out=%q err=%v\n", out, err)
			return err == nil
		},
		NewSessionFunc: func(name, dir string) error {
			fmt.Printf("TMUX NewSession(%q, %q) [suppressed]\n", name, dir)
			return nil
		},
		CommandFunc: func(args ...string) (string, error) {
			fmt.Printf("TMUX cmd: %v\n", args)
			// pass through read-only commands to real tmux
			if args[0] == "list-windows" || args[0] == "list-panes" {
				return d.Tmux.Command(args...)
			}
			return "", nil
		},
	}
	verifyDeps := *d
	verifyDeps.Tmux = mock
	res, err := LaunchVerify(&verifyDeps, cfg, target.SetRef)
	fmt.Printf("LaunchVerify result=%+v err=%v\n", res, err)
}
