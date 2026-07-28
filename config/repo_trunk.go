package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// PersistRepoTrunk records checkoutPath as the Trunk worktree for its repository
// by setting trunk = true on the global [repo."<path>"] block. The path is
// canonicalized (~ expanded, symlinks resolved) before writing.
func PersistRepoTrunk(checkoutPath string) error {
	return PersistRepoTrunkWith(defaultDeps, DefaultConfigPath(), checkoutPath)
}

// PersistRepoTrunkWith is the injectable variant.
func PersistRepoTrunkWith(d *Deps, configPath, checkoutPath string) error {
	if d == nil {
		d = defaultDeps
	}
	configPath = strings.TrimSpace(configPath)
	checkoutPath = canonicalPath(d, checkoutPath)
	if checkoutPath == "" {
		return fmt.Errorf("checkout path is required")
	}
	if configPath == "" {
		configPath = DefaultConfigPathWith(d)
	}

	doc, err := loadGlobalConfigDocument(d, configPath)
	if err != nil {
		return err
	}
	repo, ok := doc["repo"].(map[string]any)
	if !ok || repo == nil {
		repo = map[string]any{}
		doc["repo"] = repo
	}
	block, ok := repo[checkoutPath].(map[string]any)
	if !ok || block == nil {
		block = map[string]any{}
		repo[checkoutPath] = block
	}
	block["trunk"] = true
	return saveGlobalConfigDocument(d, configPath, doc)
}

func loadGlobalConfigDocument(d *Deps, configPath string) (map[string]any, error) {
	data, err := d.FS.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read config %q: %w", configPath, err)
	}
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", configPath, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func saveGlobalConfigDocument(d *Deps, configPath string, doc map[string]any) error {
	dir := filepath.Dir(configPath)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmpPath := filepath.Join(dir, fmt.Sprintf(".config.tmp-%d", os.Getpid()))
	if err := d.FS.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write config temp file: %w", err)
	}
	if err := d.FS.Rename(tmpPath, configPath); err != nil {
		_ = d.FS.RemoveAll(tmpPath)
		return fmt.Errorf("commit config: %w", err)
	}
	return nil
}
