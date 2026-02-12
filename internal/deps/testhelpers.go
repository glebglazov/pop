package deps

import (
	"io/fs"
	"os"
	"time"
)

// MockFileInfo implements os.FileInfo for testing
type MockFileInfo struct {
	NameVal    string
	SizeVal    int64
	ModeVal    os.FileMode
	ModTimeVal time.Time
	IsDirVal   bool
}

func (m MockFileInfo) Name() string       { return m.NameVal }
func (m MockFileInfo) Size() int64        { return m.SizeVal }
func (m MockFileInfo) Mode() os.FileMode  { return m.ModeVal }
func (m MockFileInfo) ModTime() time.Time { return m.ModTimeVal }
func (m MockFileInfo) IsDir() bool        { return m.IsDirVal }
func (m MockFileInfo) Sys() interface{}   { return nil }

// MockDirEntry implements os.DirEntry for testing
type MockDirEntry struct {
	NameVal  string
	IsDirVal bool
	TypeVal  os.FileMode
	InfoVal  os.FileInfo
	InfoErr  error
}

func (m MockDirEntry) Name() string               { return m.NameVal }
func (m MockDirEntry) IsDir() bool                { return m.IsDirVal }
func (m MockDirEntry) Type() os.FileMode          { return m.TypeVal }
func (m MockDirEntry) Info() (os.FileInfo, error) { return m.InfoVal, m.InfoErr }

// MockFS implements fs.FS for testing glob operations
type MockFS struct {
	Files map[string][]byte
	Dirs  map[string][]string // dir -> list of entries
}

func (m *MockFS) Open(name string) (fs.File, error) {
	if _, ok := m.Files[name]; ok {
		return &mockFile{name: name, content: m.Files[name]}, nil
	}
	if _, ok := m.Dirs[name]; ok {
		return &mockDir{name: name, entries: m.Dirs[name], fs: m}, nil
	}
	return nil, os.ErrNotExist
}

type mockFile struct {
	name    string
	content []byte
	offset  int
}

func (f *mockFile) Stat() (fs.FileInfo, error) {
	return MockFileInfo{NameVal: f.name, SizeVal: int64(len(f.content))}, nil
}
func (f *mockFile) Read(b []byte) (int, error) {
	if f.offset >= len(f.content) {
		return 0, nil
	}
	n := copy(b, f.content[f.offset:])
	f.offset += n
	return n, nil
}
func (f *mockFile) Close() error { return nil }

type mockDir struct {
	name    string
	entries []string
	fs      *MockFS
	offset  int
}

func (d *mockDir) Stat() (fs.FileInfo, error) {
	return MockFileInfo{NameVal: d.name, IsDirVal: true}, nil
}
func (d *mockDir) Read(b []byte) (int, error) { return 0, nil }
func (d *mockDir) Close() error               { return nil }

// ReadDir implements fs.ReadDirFile, required by doublestar.Glob
func (d *mockDir) ReadDir(n int) ([]fs.DirEntry, error) {
	entries := d.entries[d.offset:]
	if n > 0 && n < len(entries) {
		entries = entries[:n]
	}
	d.offset += len(entries)

	result := make([]fs.DirEntry, len(entries))
	for i, name := range entries {
		isDir := false
		if d.fs != nil {
			childPath := name
			if d.name != "." {
				childPath = d.name + "/" + name
			}
			_, isDir = d.fs.Dirs[childPath]
		}
		result[i] = MockDirEntry{NameVal: name, IsDirVal: isDir}
	}
	return result, nil
}
