package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// AtomicFS extends FileSystem with rename support for atomic writes.
type AtomicFS interface {
	Rename(oldpath, newpath string) error
}

var atomicWriteSeq uint64

func nextAtomicTempPath(dir string) string {
	seq := atomic.AddUint64(&atomicWriteSeq, 1)
	return filepath.Join(dir, fmt.Sprintf(".task-tmp-%d-%d", os.Getpid(), seq))
}

// WriteAtomic writes data to path via a same-directory temp file and atomic rename.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	return WriteAtomicWith(defaultDeps, path, data, perm)
}

// WriteAtomicWith writes data atomically using provided dependencies.
func WriteAtomicWith(d *Deps, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if afs, ok := d.FS.(AtomicFS); ok {
		tmpPath := nextAtomicTempPath(dir)
		if err := d.FS.WriteFile(tmpPath, data, perm); err != nil {
			return err
		}
		if err := afs.Rename(tmpPath, path); err != nil {
			_ = d.FS.RemoveAll(tmpPath)
			return err
		}
		return nil
	}

	f, err := os.CreateTemp(dir, ".task-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
