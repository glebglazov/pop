package deps

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FileSystem defines operations for interacting with the filesystem
type FileSystem interface {
	// Getwd returns the current working directory
	Getwd() (string, error)
	// UserHomeDir returns the user's home directory
	UserHomeDir() (string, error)
	// Getenv returns the value of an environment variable
	Getenv(key string) string
	// Stat returns file info for the given path
	Stat(path string) (os.FileInfo, error)
	// ReadDir returns directory entries for the given path
	ReadDir(path string) ([]os.DirEntry, error)
	// ReadFile returns the contents of the given file
	ReadFile(path string) ([]byte, error)
	// WriteFile writes data to the given file
	WriteFile(path string, data []byte, perm os.FileMode) error
	// MkdirAll creates a directory and all parents
	MkdirAll(path string, perm os.FileMode) error
	// DirFS returns a filesystem rooted at the given directory
	DirFS(dir string) fs.FS
	// EvalSymlinks returns the path after evaluating any symbolic links
	EvalSymlinks(path string) (string, error)
}

// RealFileSystem implements FileSystem using the real filesystem
type RealFileSystem struct{}

func NewRealFileSystem() *RealFileSystem {
	return &RealFileSystem{}
}

func (f *RealFileSystem) Getwd() (string, error) {
	return os.Getwd()
}

func (f *RealFileSystem) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

func (f *RealFileSystem) Getenv(key string) string {
	return os.Getenv(key)
}

func (f *RealFileSystem) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (f *RealFileSystem) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (f *RealFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (f *RealFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (f *RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (f *RealFileSystem) DirFS(dir string) fs.FS {
	return os.DirFS(dir)
}

func (f *RealFileSystem) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
