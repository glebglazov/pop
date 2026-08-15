package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Pop writes two config files under its data dir — config.runtime.toml and
// config.override.toml — and this file holds what the two share: reading one as
// a generic TOML document, addressing a whole key inside it by dotted path, and
// committing it back atomically or deleting it once its last key goes. What they
// do not share is their rank in the merge; that split is documented where the
// layers are loaded (applyConfigLayerMerge in merge.go).

// popWrittenFile is the identity of one pop-written config file: where it lives,
// the temp name its atomic rename passes through, and the stem its errors read
// with.
type popWrittenFile struct {
	path    string
	tmpBase string
	label   string
}

// runtimeConfigFile is config.runtime.toml, the gap-filler layer that records
// what pop's surfaces picked and so loses to every declaration of its scope.
func runtimeConfigFile(d *Deps) popWrittenFile {
	return popWrittenFile{
		path:    DefaultRuntimeConfigPathWith(d),
		tmpBase: ".config.runtime.tmp",
		label:   "runtime config",
	}
}

// overrideConfigFile is config.override.toml, the override layer that holds what
// a human stated through pop's own editor and so beats the file it overrides
// (ADR-0202).
func overrideConfigFile(d *Deps) popWrittenFile {
	return popWrittenFile{
		path:    DefaultOverrideConfigPathWith(d),
		tmpBase: ".config.override.tmp",
		label:   "override config",
	}
}

// load decodes the file as a generic TOML document, keeping keys pop does not
// know about. An absent file decodes to an empty document rather than an error,
// so every writer starts from "nothing stored yet".
func (f popWrittenFile) load(d *Deps) (map[string]any, toml.MetaData, error) {
	data, err := d.FS.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, toml.MetaData{}, nil
		}
		return nil, toml.MetaData{}, fmt.Errorf("read %s %q: %w", f.label, f.path, err)
	}
	var doc map[string]any
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return nil, toml.MetaData{}, fmt.Errorf("parse %s %q: %w", f.label, f.path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, md, nil
}

// save commits the document, or removes the file when the document has emptied
// out: a pop-written file is never left behind as an empty table, so its absence
// always means "pop stores nothing here".
func (f popWrittenFile) save(d *Deps, doc map[string]any) error {
	if len(doc) == 0 {
		if err := d.FS.RemoveAll(f.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s %q: %w", f.label, f.path, err)
		}
		return nil
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", f.label, err)
	}
	return f.writeAtomic(d, data)
}

// writeAtomic writes a temp file beside the target and renames it over, so a
// concurrent reader sees either the old file or the new one, never a
// half-written document.
func (f popWrittenFile) writeAtomic(d *Deps, data []byte) error {
	dir := filepath.Dir(f.path)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s dir: %w", f.label, err)
	}
	tmpPath := filepath.Join(dir, fmt.Sprintf("%s-%d", f.tmpBase, os.Getpid()))
	if err := d.FS.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s temp file: %w", f.label, err)
	}
	if err := d.FS.Rename(tmpPath, f.path); err != nil {
		_ = d.FS.RemoveAll(tmpPath)
		return fmt.Errorf("commit %s: %w", f.label, err)
	}
	return nil
}

// documentKeyPath splits a dotted config key ("work.implement.agents") into the
// table path a generic TOML document is walked by.
func documentKeyPath(key string) ([]string, error) {
	segments := strings.Split(key, ".")
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return nil, fmt.Errorf("config key %q is malformed: empty path segment", key)
		}
	}
	return segments, nil
}

// documentValue returns the whole value stored at a dotted key path. The bool is
// false when the key, or any table on the way to it, is absent.
func documentValue(doc map[string]any, key string) (any, bool) {
	segments, err := documentKeyPath(key)
	if err != nil {
		return nil, false
	}
	table := doc
	for _, segment := range segments[:len(segments)-1] {
		child, ok := table[segment].(map[string]any)
		if !ok {
			return nil, false
		}
		table = child
	}
	value, ok := table[segments[len(segments)-1]]
	return value, ok
}

// setDocumentValue stores value as the whole value at a dotted key path,
// creating the intermediate tables on demand. A path segment that already holds
// a non-table value is an error rather than something to overwrite: the caller
// named a key the document's shape cannot hold.
func setDocumentValue(doc map[string]any, key string, value any) error {
	segments, err := documentKeyPath(key)
	if err != nil {
		return err
	}
	table := doc
	for i, segment := range segments[:len(segments)-1] {
		switch child := table[segment].(type) {
		case map[string]any:
			table = child
		case nil:
			created := map[string]any{}
			table[segment] = created
			table = created
		default:
			return fmt.Errorf("config key %q: %q holds a value, not a table",
				key, strings.Join(segments[:i+1], "."))
		}
	}
	table[segments[len(segments)-1]] = value
	return nil
}

// deleteDocumentValue removes the whole value at a dotted key path and prunes
// every parent table the removal empties, so a document that has lost its last
// key is empty at the root and save can delete the file. It reports whether
// there was anything to remove.
func deleteDocumentValue(doc map[string]any, key string) bool {
	segments, err := documentKeyPath(key)
	if err != nil {
		return false
	}
	return deleteDocumentPath(doc, segments)
}

func deleteDocumentPath(table map[string]any, segments []string) bool {
	head := segments[0]
	if len(segments) == 1 {
		if _, ok := table[head]; !ok {
			return false
		}
		delete(table, head)
		return true
	}
	child, ok := table[head].(map[string]any)
	if !ok {
		return false
	}
	if !deleteDocumentPath(child, segments[1:]) {
		return false
	}
	if len(child) == 0 {
		delete(table, head)
	}
	return true
}
