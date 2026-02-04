package deps

import (
	"io/fs"
	"os"
)

// MockGit is a test double for Git
type MockGit struct {
	CommandFunc      func(args ...string) (string, error)
	CommandInDirFunc func(dir string, args ...string) (string, error)
}

func (m *MockGit) Command(args ...string) (string, error) {
	if m.CommandFunc != nil {
		return m.CommandFunc(args...)
	}
	return "", nil
}

func (m *MockGit) CommandInDir(dir string, args ...string) (string, error) {
	if m.CommandInDirFunc != nil {
		return m.CommandInDirFunc(dir, args...)
	}
	return "", nil
}

// MockFileSystem is a test double for FileSystem
type MockFileSystem struct {
	GetwdFunc        func() (string, error)
	UserHomeDirFunc  func() (string, error)
	GetenvFunc       func(key string) string
	StatFunc         func(path string) (os.FileInfo, error)
	ReadDirFunc      func(path string) ([]os.DirEntry, error)
	ReadFileFunc     func(path string) ([]byte, error)
	WriteFileFunc    func(path string, data []byte, perm os.FileMode) error
	MkdirAllFunc     func(path string, perm os.FileMode) error
	DirFSFunc        func(dir string) fs.FS
	EvalSymlinksFunc func(path string) (string, error)
}

func (m *MockFileSystem) Getwd() (string, error) {
	if m.GetwdFunc != nil {
		return m.GetwdFunc()
	}
	return "/mock/cwd", nil
}

func (m *MockFileSystem) UserHomeDir() (string, error) {
	if m.UserHomeDirFunc != nil {
		return m.UserHomeDirFunc()
	}
	return "/mock/home", nil
}

func (m *MockFileSystem) Getenv(key string) string {
	if m.GetenvFunc != nil {
		return m.GetenvFunc(key)
	}
	return ""
}

func (m *MockFileSystem) Stat(path string) (os.FileInfo, error) {
	if m.StatFunc != nil {
		return m.StatFunc(path)
	}
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) ReadDir(path string) ([]os.DirEntry, error) {
	if m.ReadDirFunc != nil {
		return m.ReadDirFunc(path)
	}
	return nil, nil
}

func (m *MockFileSystem) ReadFile(path string) ([]byte, error) {
	if m.ReadFileFunc != nil {
		return m.ReadFileFunc(path)
	}
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	if m.WriteFileFunc != nil {
		return m.WriteFileFunc(path, data, perm)
	}
	return nil
}

func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	if m.MkdirAllFunc != nil {
		return m.MkdirAllFunc(path, perm)
	}
	return nil
}

func (m *MockFileSystem) DirFS(dir string) fs.FS {
	if m.DirFSFunc != nil {
		return m.DirFSFunc(dir)
	}
	return nil
}

func (m *MockFileSystem) EvalSymlinks(path string) (string, error) {
	if m.EvalSymlinksFunc != nil {
		return m.EvalSymlinksFunc(path)
	}
	// Default: return path unchanged (no symlinks)
	return path, nil
}

// MockTmux is a test double for Tmux
type MockTmux struct {
	HasSessionFunc    func(name string) bool
	NewSessionFunc    func(name, dir string) error
	SwitchClientFunc  func(name string) error
	AttachSessionFunc func(name string) error
	KillSessionFunc   func(name string) error
	ListSessionsFunc  func() (string, error)
}

func (m *MockTmux) HasSession(name string) bool {
	if m.HasSessionFunc != nil {
		return m.HasSessionFunc(name)
	}
	return false
}

func (m *MockTmux) NewSession(name, dir string) error {
	if m.NewSessionFunc != nil {
		return m.NewSessionFunc(name, dir)
	}
	return nil
}

func (m *MockTmux) SwitchClient(name string) error {
	if m.SwitchClientFunc != nil {
		return m.SwitchClientFunc(name)
	}
	return nil
}

func (m *MockTmux) AttachSession(name string) error {
	if m.AttachSessionFunc != nil {
		return m.AttachSessionFunc(name)
	}
	return nil
}

func (m *MockTmux) KillSession(name string) error {
	if m.KillSessionFunc != nil {
		return m.KillSessionFunc(name)
	}
	return nil
}

func (m *MockTmux) ListSessions() (string, error) {
	if m.ListSessionsFunc != nil {
		return m.ListSessionsFunc()
	}
	return "", nil
}
