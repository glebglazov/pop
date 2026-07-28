package cmd

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/integrate"
)

const installerHome = "/home/u"

type fakeFS struct {
	files     map[string][]byte
	dirs      map[string]bool
	symlinks  map[string]string
	readErr   map[string]error
	writeErr  map[string]error
	mkdirErr  map[string]error
	removeErr map[string]error
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files:     map[string][]byte{},
		dirs:      map[string]bool{},
		symlinks:  map[string]string{},
		readErr:   map[string]error{},
		writeErr:  map[string]error{},
		mkdirErr:  map[string]error{},
		removeErr: map[string]error{},
	}
}

func (f *fakeFS) WriteFile(path string, data []byte, mode os.FileMode) error {
	return f.writeFile(path, data, mode)
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	return f.readFile(path)
}

func (f *fakeFS) MkdirAll(path string, mode os.FileMode) error {
	return f.mkdirAll(path, mode)
}

func (f *fakeFS) RemoveAll(path string) error {
	return f.removeAll(path)
}

func (f *fakeFS) Symlink(target, link string) error {
	return f.symlink(target, link)
}

func (f *fakeFS) Readlink(link string) (string, error) {
	return f.readlink(link)
}

func (f *fakeFS) LstatMode(path string) (os.FileMode, error) {
	return f.lstatMode(path)
}

func (f *fakeFS) ReadDirNames(dir string) ([]string, error) {
	return f.readDirNames(dir)
}

func (f *fakeFS) writeFile(path string, data []byte, _ os.FileMode) error {
	if err := f.writeErr[path]; err != nil {
		return err
	}
	f.files[path] = append([]byte{}, data...)
	return nil
}

func (f *fakeFS) readFile(path string) ([]byte, error) {
	if err := f.readErr[path]; err != nil {
		return nil, err
	}
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *fakeFS) mkdirAll(path string, _ os.FileMode) error {
	if err := f.mkdirErr[path]; err != nil {
		return err
	}
	f.dirs[path] = true
	return nil
}

func (f *fakeFS) removeAll(path string) error {
	if err := f.removeErr[path]; err != nil {
		return err
	}
	delete(f.dirs, path)
	delete(f.symlinks, path)
	prefix := path + string(filepath.Separator)
	for k := range f.files {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(f.files, k)
		}
	}
	for k := range f.dirs {
		if strings.HasPrefix(k, prefix) {
			delete(f.dirs, k)
		}
	}
	for k := range f.symlinks {
		if strings.HasPrefix(k, prefix) {
			delete(f.symlinks, k)
		}
	}
	return nil
}

func (f *fakeFS) symlink(target, link string) error {
	f.symlinks[link] = target
	return nil
}

func (f *fakeFS) readlink(link string) (string, error) {
	target, ok := f.symlinks[link]
	if !ok {
		return "", os.ErrNotExist
	}
	return target, nil
}

func (f *fakeFS) lstatMode(path string) (os.FileMode, error) {
	if _, ok := f.symlinks[path]; ok {
		return os.ModeSymlink, nil
	}
	if _, ok := f.files[path]; ok {
		return 0, nil
	}
	if f.dirs[path] {
		return os.ModeDir, nil
	}
	return 0, os.ErrNotExist
}

func (f *fakeFS) readDirNames(dir string) ([]string, error) {
	dir = filepath.Clean(dir)
	names := map[string]bool{}
	for p := range f.files {
		if filepath.Dir(p) == dir {
			names[filepath.Base(p)] = true
		}
	}
	for p := range f.dirs {
		if p == dir {
			continue
		}
		if filepath.Dir(p) == dir {
			names[filepath.Base(p)] = true
		}
	}
	for p := range f.symlinks {
		if filepath.Dir(p) == dir {
			names[filepath.Base(p)] = true
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func fakeDeps(home string, fs *fakeFS, stdout io.Writer) *integrate.Deps {
	return integrate.TestDeps(home, fs, stdout)
}

func paneSkillPaths() (renderFile, linkDest, linkTarget string) {
	renderRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations", "claude", "pane-skills")
	renderFile = filepath.Join(renderRoot, "pop-tmux-pane", "SKILL.md")
	linkDest = filepath.Join(installerHome, ".claude", "skills", "pop-tmux-pane")
	linkTarget = filepath.Join(renderRoot, "pop-tmux-pane")
	return
}

func stringSliceContains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
