package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RemoveCheckout removes every reachable entry before pruning Git's worktree
// administration. A failure on one entry does not prevent attempts on its
// siblings.
func RemoveCheckout(d *Deps, workingPath, checkoutPath string) error {
	if d == nil {
		d = defaultDeps
	}

	removeCheckoutTree(d, checkoutPath)
	survivors := checkoutSurvivors(d, checkoutPath)

	var removalErr error
	if len(survivors) > 0 {
		removalErr = fmt.Errorf("checkout paths survived removal:\n%s", strings.Join(survivors, "\n"))
	}
	_, pruneErr := d.Git.CommandInDir(workingPath, "worktree", "prune")
	if pruneErr != nil {
		pruneErr = fmt.Errorf("prune worktrees: %w", pruneErr)
	}
	return errors.Join(removalErr, pruneErr)
}

func removeCheckoutTree(d *Deps, path string) {
	entries, err := d.FS.ReadDir(path)
	if err == nil {
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			if entry.IsDir() {
				removeCheckoutTree(d, child)
				continue
			}
			_ = d.FS.RemoveAll(child)
		}
	}
	_ = d.FS.RemoveAll(path)
}

func checkoutSurvivors(d *Deps, path string) []string {
	if _, err := d.FS.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	var survivors []string
	var walk func(string)
	walk = func(current string) {
		survivors = append(survivors, current)
		entries, err := d.FS.ReadDir(current)
		if err != nil {
			return
		}
		for _, entry := range entries {
			child := filepath.Join(current, entry.Name())
			if entry.IsDir() {
				walk(child)
				continue
			}
			survivors = append(survivors, child)
		}
	}
	walk(path)
	sort.Strings(survivors)
	return survivors
}
