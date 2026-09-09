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

	warnCheckoutHolders(d, checkoutPath)
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

func warnCheckoutHolders(d *Deps, checkoutPath string) {
	if d.Holders == nil || d.Warn == nil {
		return
	}
	gitDir, err := d.Git.CommandInDir(checkoutPath, "rev-parse", "--git-dir")
	if err != nil {
		return
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(checkoutPath, gitDir)
	}
	holders, err := d.Holders.CheckoutHolders(checkoutPath, filepath.Clean(gitDir))
	if err != nil || len(holders) == 0 {
		return
	}
	names := make([]string, 0, len(holders))
	for _, holder := range holders {
		if holder.Name == "" {
			names = append(names, fmt.Sprintf("pid %d", holder.PID))
			continue
		}
		names = append(names, fmt.Sprintf("%s (pid %d)", holder.Name, holder.PID))
	}
	d.Warn(fmt.Sprintf("warning: removing checkout %s while held by %s", checkoutPath, strings.Join(names, ", ")))
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
