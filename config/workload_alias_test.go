package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWorkloadDeprecatedAlias_OldOnly verifies that an unchanged pre-rename
// config.toml using [workload] loads and behaves identically to its [tasks.*]
// equivalent, emitting deprecation warnings for each aliased key.
func TestWorkloadDeprecatedAlias_OldOnly(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config using the old [workload] schema
	configContent := `
[workload]
default_agents = ["claude", "codex"]

[workload.verify]
enabled = true
agents = ["verifier-agent"]
effort = "high"
max_retries = 3

[workload.git]
commit_config_overrides = ["user.name=Test User", "user.email=test@example.com"]

[workload.agents.claude]
output = "json"

[workload.agents.codex]
output = "text"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify the config was migrated to [tasks]
	if cfg.Task == nil {
		t.Fatal("Expected cfg.Task to be non-nil after migration")
	}

	// Check implement.agents
	if cfg.Task.Implement == nil {
		t.Fatal("Expected cfg.Task.Implement to be non-nil")
	}
	if len(cfg.Task.Implement.Agents) != 2 || cfg.Task.Implement.Agents[0] != "claude" || cfg.Task.Implement.Agents[1] != "codex" {
		t.Errorf("Expected implement.agents = [claude, codex], got %v", cfg.Task.Implement.Agents)
	}

	// Check verify
	if cfg.Task.Verify == nil {
		t.Fatal("Expected cfg.Task.Verify to be non-nil")
	}
	if !cfg.Task.Verify.Enabled {
		t.Error("Expected verify.enabled = true")
	}
	if len(cfg.Task.Verify.Agents) != 1 || cfg.Task.Verify.Agents[0] != "verifier-agent" {
		t.Errorf("Expected verify.agents = [verifier-agent], got %v", cfg.Task.Verify.Agents)
	}
	if cfg.Task.Verify.Effort != "high" {
		t.Errorf("Expected verify.effort = high, got %s", cfg.Task.Verify.Effort)
	}
	if cfg.Task.Verify.MaxRemediationDepth == nil || *cfg.Task.Verify.MaxRemediationDepth != 3 {
		t.Errorf("Expected verify.max_remediation_depth = 3, got %v", cfg.Task.Verify.MaxRemediationDepth)
	}

	// Check git
	if cfg.Task.Git == nil {
		t.Fatal("Expected cfg.Task.Git to be non-nil")
	}
	if len(cfg.Task.Git.CommitConfigOverrides) != 2 {
		t.Errorf("Expected git.commit_config_overrides to have 2 entries, got %d", len(cfg.Task.Git.CommitConfigOverrides))
	}

	// Check presets (agents)
	if cfg.Task.Presets == nil {
		t.Fatal("Expected cfg.Task.Presets to be non-nil")
	}
	if len(cfg.Task.Presets) != 2 {
		t.Errorf("Expected 2 presets, got %d", len(cfg.Task.Presets))
	}
	if claude, ok := cfg.Task.Presets["claude"]; !ok || claude.Output != "json" {
		t.Errorf("Expected presets.claude.output = json, got %v", cfg.Task.Presets["claude"])
	}
	if codex, ok := cfg.Task.Presets["codex"]; !ok || codex.Output != "text" {
		t.Errorf("Expected presets.codex.output = text, got %v", cfg.Task.Presets["codex"])
	}

	// Verify deprecation warnings were emitted
	expectedWarnings := []string{
		"workload",
		"tasks",
		"default_agents",
		"implement",
		"verify",
		"git",
		"agents",
		"presets",
	}

	warningText := ""
	for _, f := range cfg.Findings {
		warningText += f.Message + " "
	}

	for _, keyword := range expectedWarnings {
		found := false
		for _, f := range cfg.Findings {
			if contains(f.Message, keyword) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected deprecation warning containing %q, but not found in findings: %v", keyword, cfg.Findings)
		}
	}
}

// TestWorkloadDeprecatedAlias_NewOnly verifies that a config using only [tasks]
// works without any deprecation warnings.
func TestWorkloadDeprecatedAlias_NewOnly(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config using the new [tasks] schema
	configContent := `
[tasks.implement]
agents = ["claude", "codex"]

[tasks.verify]
enabled = true
agents = ["verifier-agent"]
effort = "high"
max_remediation_depth = 3

[tasks.git]
commit_config_overrides = ["user.name=Test User", "user.email=test@example.com"]

[tasks.presets.claude]
output = "json"

[tasks.presets.codex]
output = "text"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify the config loaded correctly
	if cfg.Task == nil {
		t.Fatal("Expected cfg.Task to be non-nil")
	}
	if cfg.Task.Implement == nil || len(cfg.Task.Implement.Agents) != 2 {
		t.Errorf("Expected implement.agents to have 2 entries")
	}
	if cfg.Task.Verify == nil || !cfg.Task.Verify.Enabled {
		t.Error("Expected verify.enabled = true")
	}
	if cfg.Task.Git == nil || len(cfg.Task.Git.CommitConfigOverrides) != 2 {
		t.Errorf("Expected git.commit_config_overrides to have 2 entries")
	}
	if cfg.Task.Presets == nil || len(cfg.Task.Presets) != 2 {
		t.Errorf("Expected 2 presets")
	}

	// Verify NO deprecation warnings were emitted
	for _, f := range cfg.Findings {
		if contains(f.Message, "workload") && contains(f.Message, "deprecated") {
			t.Errorf("Unexpected deprecation warning for workload: %s", f.Message)
		}
	}
}

// TestWorkloadDeprecatedAlias_MixedPrecedence verifies that when both old and
// new keys are present, the new [tasks] values win and the old [workload]
// values are ignored (with warnings).
func TestWorkloadDeprecatedAlias_MixedPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config with both [workload] and [tasks] for the same keys
	configContent := `
[workload]
default_agents = ["old-agent"]

[workload.verify]
enabled = false
agents = ["old-verifier"]
effort = "low"
max_retries = 1

[workload.git]
commit_config_overrides = ["old.override=old"]

[workload.agents.old-preset]
output = "old-output"

[tasks.implement]
agents = ["new-agent"]

[tasks.verify]
enabled = true
agents = ["new-verifier"]
effort = "high"
max_remediation_depth = 5

[tasks.git]
commit_config_overrides = ["new.override=new"]

[tasks.presets.new-preset]
output = "new-output"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify [tasks] values win (new-wins precedence)
	if cfg.Task == nil {
		t.Fatal("Expected cfg.Task to be non-nil")
	}

	// Check implement.agents - should be new value
	if cfg.Task.Implement == nil || len(cfg.Task.Implement.Agents) != 1 || cfg.Task.Implement.Agents[0] != "new-agent" {
		t.Errorf("Expected implement.agents = [new-agent] (new wins), got %v", cfg.Task.Implement.Agents)
	}

	// Check verify - should be new values
	if cfg.Task.Verify == nil {
		t.Fatal("Expected cfg.Task.Verify to be non-nil")
	}
	if !cfg.Task.Verify.Enabled {
		t.Error("Expected verify.enabled = true (new wins)")
	}
	if len(cfg.Task.Verify.Agents) != 1 || cfg.Task.Verify.Agents[0] != "new-verifier" {
		t.Errorf("Expected verify.agents = [new-verifier] (new wins), got %v", cfg.Task.Verify.Agents)
	}
	if cfg.Task.Verify.Effort != "high" {
		t.Errorf("Expected verify.effort = high (new wins), got %s", cfg.Task.Verify.Effort)
	}
	if cfg.Task.Verify.MaxRemediationDepth == nil || *cfg.Task.Verify.MaxRemediationDepth != 5 {
		t.Errorf("Expected verify.max_remediation_depth = 5 (new wins), got %v", cfg.Task.Verify.MaxRemediationDepth)
	}

	// Check git - should be new value
	if cfg.Task.Git == nil || len(cfg.Task.Git.CommitConfigOverrides) != 1 || cfg.Task.Git.CommitConfigOverrides[0] != "new.override=new" {
		t.Errorf("Expected git.commit_config_overrides = [new.override=new] (new wins), got %v", cfg.Task.Git.CommitConfigOverrides)
	}

	// Check presets - should have both old and new presets (different names)
	// Only preset names that exist in both would follow new-wins precedence
	if cfg.Task.Presets == nil {
		t.Fatal("Expected cfg.Task.Presets to be non-nil")
	}
	if len(cfg.Task.Presets) != 2 {
		t.Errorf("Expected 2 presets (old-preset from workload, new-preset from tasks), got %d", len(cfg.Task.Presets))
	}
	if newPreset, ok := cfg.Task.Presets["new-preset"]; !ok || newPreset.Output != "new-output" {
		t.Errorf("Expected presets.new-preset.output = new-output, got %v", cfg.Task.Presets["new-preset"])
	}
	if oldPreset, ok := cfg.Task.Presets["old-preset"]; !ok || oldPreset.Output != "old-output" {
		t.Errorf("Expected presets.old-preset.output = old-output (from workload), got %v", cfg.Task.Presets["old-preset"])
	}
}

// TestWorkloadDeprecatedAlias_PresetNameCollision verifies that when the same
// preset name exists in both [workload.agents] and [tasks.presets], the new
// value wins (new-wins per-key semantics).
func TestWorkloadDeprecatedAlias_PresetNameCollision(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config where the same preset name exists in both old and new
	configContent := `
[workload.agents.shared-preset]
output = "old-output"

[tasks.presets.shared-preset]
output = "new-output"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify new wins for the shared preset name
	if cfg.Task == nil || cfg.Task.Presets == nil {
		t.Fatal("Expected cfg.Task.Presets to be non-nil")
	}
	if len(cfg.Task.Presets) != 1 {
		t.Errorf("Expected 1 preset, got %d", len(cfg.Task.Presets))
	}
	if preset, ok := cfg.Task.Presets["shared-preset"]; !ok || preset.Output != "new-output" {
		t.Errorf("Expected presets.shared-preset.output = new-output (new wins), got %v", cfg.Task.Presets["shared-preset"])
	}

	// Verify deprecation warnings were still emitted
	foundWorkloadWarning := false
	for _, f := range cfg.Findings {
		if contains(f.Message, "workload") && contains(f.Message, "deprecated") {
			foundWorkloadWarning = true
			break
		}
	}
	if !foundWorkloadWarning {
		t.Error("Expected deprecation warning for workload even when new keys are present")
	}
}

// TestWorkloadDeprecatedAlias_PartialOverride verifies that when only some
// keys are present in [tasks], the missing keys fall back to [workload] values.
func TestWorkloadDeprecatedAlias_PartialOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config with [workload] fully defined, but [tasks] only partially defined
	configContent := `
[workload]
default_agents = ["old-agent"]

[workload.verify]
enabled = true
agents = ["old-verifier"]
effort = "low"
max_retries = 2

[workload.git]
commit_config_overrides = ["old.override=old"]

[tasks.implement]
agents = ["new-agent"]
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Task == nil {
		t.Fatal("Expected cfg.Task to be non-nil")
	}

	// Check implement.agents - should be new value (explicit override)
	if cfg.Task.Implement == nil || len(cfg.Task.Implement.Agents) != 1 || cfg.Task.Implement.Agents[0] != "new-agent" {
		t.Errorf("Expected implement.agents = [new-agent] (new wins), got %v", cfg.Task.Implement.Agents)
	}

	// Check verify - should fall back to old values (not overridden)
	if cfg.Task.Verify == nil {
		t.Fatal("Expected cfg.Task.Verify to be non-nil (migrated from workload)")
	}
	if !cfg.Task.Verify.Enabled {
		t.Error("Expected verify.enabled = true (from workload)")
	}
	if len(cfg.Task.Verify.Agents) != 1 || cfg.Task.Verify.Agents[0] != "old-verifier" {
		t.Errorf("Expected verify.agents = [old-verifier] (from workload), got %v", cfg.Task.Verify.Agents)
	}
	if cfg.Task.Verify.Effort != "low" {
		t.Errorf("Expected verify.effort = low (from workload), got %s", cfg.Task.Verify.Effort)
	}
	if cfg.Task.Verify.MaxRemediationDepth == nil || *cfg.Task.Verify.MaxRemediationDepth != 2 {
		t.Errorf("Expected verify.max_remediation_depth = 2 (from workload), got %v", cfg.Task.Verify.MaxRemediationDepth)
	}

	// Check git - should fall back to old value (not overridden)
	if cfg.Task.Git == nil || len(cfg.Task.Git.CommitConfigOverrides) != 1 || cfg.Task.Git.CommitConfigOverrides[0] != "old.override=old" {
		t.Errorf("Expected git.commit_config_overrides = [old.override=old] (from workload), got %v", cfg.Task.Git.CommitConfigOverrides)
	}
}

// TestWorkloadDeprecatedAlias_Includes verifies that an includes file using
// deprecated [workload] blocks still contributes them (with warnings) and
// folds them into the new [tasks] shape.
func TestWorkloadDeprecatedAlias_Includes(t *testing.T) {
	tmpDir := t.TempDir()
	mainConfigPath := filepath.Join(tmpDir, "config.toml")
	includesDir := filepath.Join(tmpDir, "includes")
	if err := os.MkdirAll(includesDir, 0755); err != nil {
		t.Fatalf("Failed to create includes directory: %v", err)
	}
	includedConfigPath := filepath.Join(includesDir, "workload.toml")

	// Write main config that includes the workload file
	mainConfigContent := `
includes = ["` + filepath.ToSlash(includedConfigPath) + `"]

[tasks.implement]
agents = ["main-agent"]
`
	if err := os.WriteFile(mainConfigPath, []byte(mainConfigContent), 0644); err != nil {
		t.Fatalf("Failed to write main config: %v", err)
	}

	// Write included config using deprecated [workload] schema
	includedConfigContent := `
[workload.verify]
enabled = true
agents = ["included-verifier"]
effort = "medium"

[workload.git]
commit_config_overrides = ["included.override=included"]

[workload.agents.included-preset]
output = "included-output"
`
	if err := os.WriteFile(includedConfigPath, []byte(includedConfigContent), 0644); err != nil {
		t.Fatalf("Failed to write included config: %v", err)
	}

	cfg, err := Load(mainConfigPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Task == nil {
		t.Fatal("Expected cfg.Task to be non-nil")
	}

	// Check implement.agents - should be from main config
	if cfg.Task.Implement == nil || len(cfg.Task.Implement.Agents) != 1 || cfg.Task.Implement.Agents[0] != "main-agent" {
		t.Errorf("Expected implement.agents = [main-agent], got %v", cfg.Task.Implement.Agents)
	}

	// Check verify - should be from included workload config
	if cfg.Task.Verify == nil {
		t.Fatal("Expected cfg.Task.Verify to be non-nil (from included workload)")
	}
	if !cfg.Task.Verify.Enabled {
		t.Error("Expected verify.enabled = true (from included workload)")
	}
	if len(cfg.Task.Verify.Agents) != 1 || cfg.Task.Verify.Agents[0] != "included-verifier" {
		t.Errorf("Expected verify.agents = [included-verifier], got %v", cfg.Task.Verify.Agents)
	}
	if cfg.Task.Verify.Effort != "medium" {
		t.Errorf("Expected verify.effort = medium, got %s", cfg.Task.Verify.Effort)
	}

	// Check git - should be from included workload config
	if cfg.Task.Git == nil || len(cfg.Task.Git.CommitConfigOverrides) != 1 || cfg.Task.Git.CommitConfigOverrides[0] != "included.override=included" {
		t.Errorf("Expected git.commit_config_overrides = [included.override=included], got %v", cfg.Task.Git.CommitConfigOverrides)
	}

	// Check presets - should be from included workload config
	if cfg.Task.Presets == nil || len(cfg.Task.Presets) != 1 {
		t.Fatalf("Expected 1 preset from included workload")
	}
	if includedPreset, ok := cfg.Task.Presets["included-preset"]; !ok || includedPreset.Output != "included-output" {
		t.Errorf("Expected presets.included-preset.output = included-output, got %v", cfg.Task.Presets["included-preset"])
	}

	// Verify deprecation warnings were emitted for the included file
	foundWorkloadWarning := false
	for _, f := range cfg.Findings {
		if contains(f.Message, "workload") && contains(f.Message, "deprecated") {
			foundWorkloadWarning = true
			break
		}
	}
	if !foundWorkloadWarning {
		t.Error("Expected deprecation warning for workload in included file")
	}
}

// TestWorkloadDeprecatedAlias_CriticalGitOverride specifically verifies that
// a leftover [workload.git].commit_config_overrides is still honored, which
// guards against the silent GPG-signing re-arm scenario.
func TestWorkloadDeprecatedAlias_CriticalGitOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config with only [workload.git] commit_config_overrides
	configContent := `
[workload.git]
commit_config_overrides = ["commit.gpgsign=false", "user.signingkey="]
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify the git overrides were migrated
	if cfg.Task == nil || cfg.Task.Git == nil {
		t.Fatal("Expected cfg.Task.Git to be non-nil after migration")
	}
	if len(cfg.Task.Git.CommitConfigOverrides) != 2 {
		t.Errorf("Expected 2 commit_config_overrides, got %d", len(cfg.Task.Git.CommitConfigOverrides))
	}
	if cfg.Task.Git.CommitConfigOverrides[0] != "commit.gpgsign=false" {
		t.Errorf("Expected first override = commit.gpgsign=false, got %s", cfg.Task.Git.CommitConfigOverrides[0])
	}
	if cfg.Task.Git.CommitConfigOverrides[1] != "user.signingkey=" {
		t.Errorf("Expected second override = user.signingkey=, got %s", cfg.Task.Git.CommitConfigOverrides[1])
	}

	// Verify ResolveCommitConfigOverrides works with migrated config
	overrides, err := cfg.ResolveCommitConfigOverrides()
	if err != nil {
		t.Errorf("ResolveCommitConfigOverrides failed: %v", err)
	}
	if len(overrides) != 2 {
		t.Errorf("Expected 2 resolved overrides, got %d", len(overrides))
	}
}

// TestWorkloadDeprecatedAlias_CriticalVerifyEnabled specifically verifies that
// a leftover [workload.verify].enabled = true still enables verification.
func TestWorkloadDeprecatedAlias_CriticalVerifyEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config with only [workload.verify] enabled
	configContent := `
[workload.verify]
enabled = true
agents = ["verifier"]
effort = "high"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify the verify config was migrated
	if cfg.Task == nil || cfg.Task.Verify == nil {
		t.Fatal("Expected cfg.Task.Verify to be non-nil after migration")
	}
	if !cfg.Task.Verify.Enabled {
		t.Error("Expected verify.enabled = true to be preserved")
	}
	if len(cfg.Task.Verify.Agents) != 1 || cfg.Task.Verify.Agents[0] != "verifier" {
		t.Errorf("Expected verify.agents = [verifier], got %v", cfg.Task.Verify.Agents)
	}
	if cfg.Task.Verify.Effort != "high" {
		t.Errorf("Expected verify.effort = high, got %s", cfg.Task.Verify.Effort)
	}
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

// findSubstring performs a case-insensitive substring search.
func findSubstring(s, substr string) bool {
	sLower := toLower(s)
	substrLower := toLower(substr)
	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		if sLower[i:i+len(substrLower)] == substrLower {
			return true
		}
	}
	return false
}

// toLower converts a string to lowercase.
func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + ('a' - 'A')
		} else {
			result[i] = c
		}
	}
	return string(result)
}
