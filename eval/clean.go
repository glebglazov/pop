package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func runCleanCommand(args []string) error {
	flags := flag.NewFlagSet("clean", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	work := flags.String("work", defaultWorkRoot, "Eval work directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: go run ./eval clean [--work <path>]")
	}
	return cleanEvalWork(*work, os.Stdout)
}

func cleanEvalWork(root string, out io.Writer) error {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "Eval work root is absent: %s; reclaimed 0 bytes\n", root)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Eval work root %s: %w", root, err)
	}

	removed := 0
	var reclaimed int64
	for _, entry := range entries {
		if !entry.IsDir() || !disposableEvalWorkDir(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		size, err := directorySize(path)
		if err != nil {
			return fmt.Errorf("measure Eval work directory %s: %w", path, err)
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove Eval work directory %s: %w", path, err)
		}
		removed++
		reclaimed += size
	}
	if removed == 0 {
		fmt.Fprintf(out, "Eval work root has no disposable work: %s; reclaimed 0 bytes\n", root)
		return nil
	}
	fmt.Fprintf(out, "Cleaned %d Eval work directories from %s; reclaimed %d bytes\n", removed, root, reclaimed)
	return nil
}

func disposableEvalWorkDir(name string) bool {
	return strings.HasPrefix(name, "trial-") || strings.Contains(name, "-acceptance-")
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}
