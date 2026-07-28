package integrate

import (
	"bytes"
	"os"
	"strings"
)

// run is the per-invocation execution context: injection (via deps), intent
// flags copied from Request, and accumulating outcome. It is never exported —
// callers use Install/Remove and read Report.
type run struct {
	deps *Deps

	dryRun             bool
	overwriteConflicts bool
	assumeYes          bool
	agentName          string

	changed        bool
	installed      bool
	overwrotePaths []string
	prunedStale    []string
}

func newRun(d *Deps, req Request) *run {
	r := &run{
		deps:               d,
		dryRun:             req.DryRun,
		overwriteConflicts: req.OverwriteConflicts,
		assumeYes:          req.AssumeYes,
		agentName:          strings.ToLower(req.Agent),
	}
	if req.DryRun {
		r.deps = applyDryRunSeams(d, r)
	}
	return r
}

func (r *run) toReport(outcomes []Outcome) Report {
	return Report{
		Changed:     r.changed,
		Installed:   r.installed,
		Overwritten: append([]string(nil), r.overwrotePaths...),
		Pruned:      append([]string(nil), r.prunedStale...),
		Outcomes:    outcomes,
	}
}

// applyDryRunSeams returns a Deps copy whose write paths compare rather than
// mutate, recording installed/changed on r. Private to the run — there is no
// exported dry-run dependency-clone helper.
func applyDryRunSeams(base *Deps, r *run) *Deps {
	d := &Deps{
		userHomeDir:      base.userHomeDir,
		readFile:         base.readFile,
		dataDir:          base.dataDir,
		logf:             base.logf,
		skillsPrefix:     base.skillsPrefix,
		readDirNames:     base.readDirNames,
		getenv:           base.getenv,
		ConfirmOverwrite: base.ConfirmOverwrite,
		readlink:         base.readlink,
		lstatMode:        base.lstatMode,
		symlink:          func(string, string) error { return nil },
		mkdirAll:         func(string, os.FileMode) error { return nil },
		removeAll:        func(string) error { return nil },
		stdout:           nil,
	}
	d.writeFile = func(path string, data []byte, _ os.FileMode) error {
		existing, err := d.readFile(path)
		if err == nil {
			r.installed = true
			if !bytes.Equal(existing, data) {
				r.changed = true
			}
		}
		return nil
	}
	return d
}
