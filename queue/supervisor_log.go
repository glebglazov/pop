package queue

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/glebglazov/pop/tasks"
)

const (
	supervisorLogName       = "supervisor.log"
	supervisorLogMaxBytes   = 1 << 20
	supervisorLogMaxBackups = 3
)

// SupervisorLogPath returns the durable free-form supervisor narration path.
func SupervisorLogPath(d *tasks.Deps) string {
	return filepath.Join(QueueDataDir(d), supervisorLogName)
}

func supervisorOutput(d *tasks.Deps, out io.Writer) (io.Writer, io.Closer, error) {
	log, err := newRotatingSupervisorLog(d, supervisorLogMaxBytes, supervisorLogMaxBackups)
	if err != nil {
		return nil, nil, err
	}
	return io.MultiWriter(out, log), log, nil
}

type rotatingSupervisorLog struct {
	path    string
	maxSize int64
	backups int
	file    *os.File
	size    int64
}

func newRotatingSupervisorLog(d *tasks.Deps, maxSize int64, backups int) (*rotatingSupervisorLog, error) {
	if err := d.FS.MkdirAll(QueueDataDir(d), 0o755); err != nil {
		return nil, fmt.Errorf("create queue data dir: %w", err)
	}
	w := &rotatingSupervisorLog{
		path:    SupervisorLogPath(d),
		maxSize: maxSize,
		backups: backups,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	if w.maxSize > 0 && w.size >= w.maxSize {
		if err := w.rotate(); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	return w, nil
}

func (w *rotatingSupervisorLog) Write(p []byte) (int, error) {
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.maxSize > 0 && w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingSupervisorLog) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingSupervisorLog) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open supervisor log: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat supervisor log: %w", err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *rotatingSupervisorLog) rotate() error {
	if err := w.Close(); err != nil {
		return fmt.Errorf("close supervisor log before rotation: %w", err)
	}
	if w.backups > 0 {
		_ = os.Remove(rotatedSupervisorLogPath(w.path, w.backups))
		for i := w.backups - 1; i >= 1; i-- {
			oldPath := rotatedSupervisorLogPath(w.path, i)
			newPath := rotatedSupervisorLogPath(w.path, i+1)
			if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("rotate supervisor log backup: %w", err)
			}
		}
		if err := os.Rename(w.path, rotatedSupervisorLogPath(w.path, 1)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate supervisor log: %w", err)
		}
	} else if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("truncate supervisor log: %w", err)
	}
	return w.open()
}

func rotatedSupervisorLogPath(path string, generation int) string {
	return fmt.Sprintf("%s.%d", path, generation)
}
