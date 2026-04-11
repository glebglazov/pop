package cmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/debug"
	"github.com/spf13/cobra"
)

//go:embed skills/pop/*.md
var skillFiles embed.FS

//go:embed extensions/pi/pop-status-sync.ts
var piExtensionFile []byte

//go:embed extensions/opencode/pop-status-sync.ts
var opencodeExtensionFile []byte

// integrateDeps holds the filesystem operations the integrate command depends
// on. Production code uses defaultIntegrateDeps; tests inject mocks.
//
// The same struct drives two modes:
//
//   - Real mode: writeFile/mkdirAll/removeAll mutate the filesystem.
//   - Dry-run mode (DryRun=true, constructed via withDryRun): writeFile
//     compares the would-be bytes against what's on disk and records flags
//     instead of writing. The install functions do not branch on DryRun —
//     they just run unchanged and the deps layer short-circuits side effects.
//
// The dry-run output fields are written only in dry-run mode:
//
//   - installed: set to true when any pop artifact already exists for this
//     agent (a file would be overwritten by the real run).
//   - changed:   set to true when at least one write would produce bytes
//     that differ from what's on disk.
//
// Agent-specific install logic that knows something the deps shim can't see
// (e.g. "there are pop hooks inside settings.json") may set `installed`
// directly, guarded by `d.DryRun`. See installClaudeHooks for an example.
type integrateDeps struct {
	userHomeDir func() (string, error)
	readFile    func(string) ([]byte, error)
	writeFile   func(string, []byte, os.FileMode) error
	mkdirAll    func(string, os.FileMode) error
	removeAll   func(string) error
	stdout      io.Writer

	// Dry-run mode: set DryRun=true to turn writeFile into a comparator.
	// `installed` and `changed` are output fields filled in during the run.
	DryRun    bool
	changed   bool
	installed bool
}

func defaultIntegrateDeps() *integrateDeps {
	return &integrateDeps{
		userHomeDir: os.UserHomeDir,
		readFile:    os.ReadFile,
		writeFile:   os.WriteFile,
		mkdirAll:    os.MkdirAll,
		removeAll:   os.RemoveAll,
		stdout:      os.Stdout,
	}
}

// dryRunIntegrateDeps returns an integrateDeps that reports what would change
// on disk without performing any writes. See the integrateDeps doc for the
// semantics of `installed` and `changed`.
func dryRunIntegrateDeps() *integrateDeps {
	return withDryRun(defaultIntegrateDeps())
}

// withDryRun wraps a base integrateDeps with dry-run behavior. It is exposed
// as a separate function so tests can layer dry-run on top of a fake FS
// without touching the real filesystem.
func withDryRun(base *integrateDeps) *integrateDeps {
	d := &integrateDeps{
		userHomeDir: base.userHomeDir,
		readFile:    base.readFile,
		DryRun:      true,
	}
	// writeFile compares the proposed bytes against what's on disk.
	// Existing file → installed; different content → changed.
	// Missing file → neither (creating new files on an agent that isn't
	// installed yet is not an "update"; the auto-updater should skip).
	d.writeFile = func(path string, data []byte, _ os.FileMode) error {
		existing, err := d.readFile(path)
		if err == nil {
			d.installed = true
			if !bytes.Equal(existing, data) {
				d.changed = true
			}
		}
		return nil
	}
	// mkdirAll is a no-op in dry-run; directory creation is not a meaningful
	// change signal on its own (the file write inside catches the real state).
	d.mkdirAll = func(string, os.FileMode) error { return nil }
	// removeAll is a no-op in dry-run. We intentionally do not probe the
	// target with os.Stat here so the dry-run path stays injectable by
	// tests (which swap readFile/writeFile on a fake FS but do not stub
	// os.Stat). In practice, installed/changed detection relies on the
	// writeFile comparator — every install step that removes a directory
	// is followed by writes into that directory, which provide the signal.
	d.removeAll = func(string) error { return nil }
	// Suppress output from install functions during dry-run.
	d.stdout = nil
	return d
}

// integrateUpdateExisting, when true, tells integrateCmd to refresh any
// already-installed agent integrations to match the current binary's
// embedded content instead of installing for a specific agent.
var integrateUpdateExisting bool

var integrateCmd = &cobra.Command{
	Use:   "integrate [agent]",
	Short: "Install pop integrations for a coding agent",
	Long: `Install pop integrations (skills + pane-status hooks) for a coding agent.

Supported agents:
  claude    Install slash commands at ~/.claude/commands/pop/ and pane
            monitoring hooks in ~/.claude/settings.json.
  pi        Install skills at ~/.pi/agent/skills/pop-<name>/SKILL.md and a
            pane monitoring extension at
            ~/.pi/agent/extensions/pop-status-sync.ts.
  opencode  Install skills at ~/.config/opencode/agent/pop-<name>.md and a
            pane monitoring plugin at
            ~/.config/opencode/plugins/pop-status-sync.ts.

Re-running the command for an agent is idempotent: existing pop integration
files for that agent are replaced with the current versions, and unrelated
hooks/skills are preserved.

With --update-existing, no agent argument is expected: pop detects which
agents are already integrated and refreshes them to the current binary's
embedded content. Agents that are not installed are left alone. This is
the command that 'make install' and the Homebrew post_install hook run
after copying a new binary into place.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if integrateUpdateExisting {
			if len(args) > 0 {
				return fmt.Errorf("--update-existing does not accept an agent argument")
			}
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("requires exactly 1 argument: agent name (claude, pi, or opencode)")
		}
		return nil
	},
	ValidArgs: []string{"claude", "pi", "opencode"},
	RunE:      runIntegrate,
}

func init() {
	integrateCmd.Flags().BoolVar(&integrateUpdateExisting, "update-existing", false,
		"Refresh already-installed agent integrations to match the current binary (no agent argument)")
	rootCmd.AddCommand(integrateCmd)
}

func runIntegrate(cmd *cobra.Command, args []string) error {
	if integrateUpdateExisting {
		return runIntegrateUpdateExisting()
	}
	return runIntegrateWith(defaultIntegrateDeps(), args[0])
}

func runIntegrateWith(d *integrateDeps, agent string) error {
	switch strings.ToLower(agent) {
	case "claude":
		return integrateClaude(d)
	case "pi":
		return integratePi(d)
	case "opencode":
		return integrateOpencode(d)
	default:
		return fmt.Errorf("unknown agent %q (expected: claude, pi, opencode)", agent)
	}
}

// ----- Claude integration ----------------------------------------------------

// popHooks defines the hook commands installed into Claude's settings.json.
// Each entry is a (event, command) pair; the matcher is left empty so the
// hook fires for every tool / event.
var popHooks = []struct {
	event   string
	command string
}{
	{"SessionStart", "pop pane set-status idle 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-status working 2>/dev/null || true"},
	{"PreToolUse", "pop pane set-status working 2>/dev/null || true"},
	{"Stop", "pop pane set-status unread 2>/dev/null || true"},
	{"Notification", "pop pane set-status unread 2>/dev/null || true"},
}

func integrateClaude(d *integrateDeps) error {
	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if err := installClaudeCommands(d, home); err != nil {
		return err
	}
	if err := installClaudeHooks(d, home); err != nil {
		return err
	}
	return nil
}

// installClaudeCommands writes the embedded skills as Claude slash commands
// under ~/.claude/commands/pop/. The directory is wiped first so removed
// skills do not linger.
func installClaudeCommands(d *integrateDeps, home string) error {
	commandsDir := filepath.Join(home, ".claude", "commands", "pop")
	if err := d.removeAll(commandsDir); err != nil {
		return fmt.Errorf("failed to clean %s: %w", commandsDir, err)
	}
	if err := d.mkdirAll(commandsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", commandsDir, err)
	}

	entries, err := skillFiles.ReadDir("skills/pop")
	if err != nil {
		return fmt.Errorf("failed to read embedded skills: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := skillFiles.ReadFile("skills/pop/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read skill %s: %w", entry.Name(), err)
		}
		dest := filepath.Join(commandsDir, entry.Name())
		if err := d.writeFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  installed /pop:%s\n", strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	return nil
}

// installClaudeHooks merges pop's hook entries into ~/.claude/settings.json,
// preserving any unrelated existing hooks. Old pop hooks are removed first
// (matched via isPopHook) so re-running the command is idempotent.
func installClaudeHooks(d *integrateDeps, home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	settings := make(map[string]interface{})
	data, err := d.readFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	// Strip any previously installed pop hooks before adding the current set.
	for event, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		cleaned := removePopHooks(eventHooks)
		// Dry-run "installed" detection for claude: settings.json exists for
		// every claude user, so file-presence is not a reliable signal that
		// pop is installed. Finding any existing pop hooks is — they could
		// only have gotten there via a prior `pop integrate claude` run.
		if d.DryRun && len(cleaned) < len(eventHooks) {
			d.installed = true
		}
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}

	for _, h := range popHooks {
		hookEntry := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": h.command,
				},
			},
		}
		eventHooks, _ := hooks[h.event].([]interface{})
		eventHooks = append(eventHooks, hookEntry)
		hooks[h.event] = eventHooks
	}

	if err := d.mkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	if err := d.writeFile(settingsPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsPath, err)
	}

	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed %d hook(s) in %s\n", len(popHooks), settingsPath)
	}
	return nil
}

// ----- Pi integration --------------------------------------------------------

func integratePi(d *integrateDeps) error {
	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if err := installPiExtension(d, home); err != nil {
		return err
	}
	if err := installPiSkills(d, home); err != nil {
		return err
	}
	return nil
}

// installPiExtension writes the embedded pi extension TypeScript file. Pi
// auto-discovers any *.ts file under ~/.pi/agent/extensions/ at startup.
func installPiExtension(d *integrateDeps, home string) error {
	extDir := filepath.Join(home, ".pi", "agent", "extensions")
	if err := d.mkdirAll(extDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", extDir, err)
	}
	extPath := filepath.Join(extDir, "pop-status-sync.ts")
	if err := d.writeFile(extPath, piExtensionFile, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", extPath, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed pi extension at %s\n", extPath)
	}
	return nil
}

// installPiSkills writes each embedded skill as a pi skill directory under
// ~/.pi/agent/skills/pop-<basename>/SKILL.md. Pi requires the frontmatter
// `name` field to match the parent directory; we inject it on install so the
// source files stay agent-agnostic.
func installPiSkills(d *integrateDeps, home string) error {
	entries, err := skillFiles.ReadDir("skills/pop")
	if err != nil {
		return fmt.Errorf("failed to read embedded skills: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := skillFiles.ReadFile("skills/pop/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read skill %s: %w", entry.Name(), err)
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		piName := "pop-" + base
		dir := filepath.Join(home, ".pi", "agent", "skills", piName)
		// Wipe any prior install of this exact skill so a renamed file does
		// not leave a stale SKILL.md behind.
		if err := d.removeAll(dir); err != nil {
			return fmt.Errorf("failed to clean %s: %w", dir, err)
		}
		if err := d.mkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
		dest := filepath.Join(dir, "SKILL.md")
		content := injectFrontmatterName(string(data), piName)
		if err := d.writeFile(dest, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  installed pi skill %s\n", piName)
		}
	}
	return nil
}

// injectFrontmatterName guarantees the YAML frontmatter contains a `name:`
// field set to the given value. If the file already has a name, it is
// replaced. If there is no frontmatter at all, one is created.
func injectFrontmatterName(content, name string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		// No frontmatter — wrap the content in one.
		return fmt.Sprintf("---\nname: %s\n---\n%s", name, content)
	}
	// Find the closing `---`.
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		// Malformed frontmatter — leave it alone.
		return content
	}
	// Replace existing name: line if present.
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "name:") {
			lines[i] = "name: " + name
			return strings.Join(lines, "\n")
		}
	}
	// Otherwise insert `name:` directly after the opening `---`.
	out := append([]string{lines[0], "name: " + name}, lines[1:]...)
	return strings.Join(out, "\n")
}

// ----- Opencode integration --------------------------------------------------

func integrateOpencode(d *integrateDeps) error {
	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if err := installOpencodePlugin(d, home); err != nil {
		return err
	}
	if err := installOpencodeSkills(d, home); err != nil {
		return err
	}
	return nil
}

// installOpencodePlugin writes the embedded opencode plugin TypeScript file.
// Opencode auto-discovers any *.ts file under ~/.config/opencode/plugins/ at startup.
func installOpencodePlugin(d *integrateDeps, home string) error {
	pluginDir := filepath.Join(home, ".config", "opencode", "plugins")
	if err := d.mkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", pluginDir, err)
	}
	pluginPath := filepath.Join(pluginDir, "pop-status-sync.ts")
	if err := d.writeFile(pluginPath, opencodeExtensionFile, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", pluginPath, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed opencode plugin at %s\n", pluginPath)
	}
	return nil
}

// installOpencodeSkills writes each embedded skill as an opencode agent markdown
// file under ~/.config/opencode/agent/pop-<basename>.md.
func installOpencodeSkills(d *integrateDeps, home string) error {
	entries, err := skillFiles.ReadDir("skills/pop")
	if err != nil {
		return fmt.Errorf("failed to read embedded skills: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := skillFiles.ReadFile("skills/pop/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read skill %s: %w", entry.Name(), err)
		}
		base := strings.TrimSuffix(entry.Name(), ".md")
		opencodeName := "pop-" + base
		agentDir := filepath.Join(home, ".config", "opencode", "agent")
		if err := d.mkdirAll(agentDir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", agentDir, err)
		}
		dest := filepath.Join(agentDir, opencodeName+".md")
		if err := d.writeFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  installed opencode agent %s\n", opencodeName)
		}
	}
	return nil
}

// ----- Shared helpers --------------------------------------------------------

// removePopHooks filters out hook entries whose commands look like pop
// monitoring commands. Used to deduplicate when re-installing.
func removePopHooks(entries []interface{}) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		if !isPopHook(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// isPopHook returns true if any command in the hook entry references one of
// the pop pane-monitoring commands.
func isPopHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	innerHooks, _ := entryMap["hooks"].([]interface{})
	for _, h := range innerHooks {
		hMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if cmd, _ := hMap["command"].(string); strings.Contains(cmd, "pop monitor") || strings.Contains(cmd, "pop pane set-status") {
			return true
		}
	}
	return false
}

// ----- App state (state.json) ------------------------------------------------

// appState holds cross-run markers persisted at ~/.local/share/pop/state.json.
// Currently used only as a staleness marker for auto-updating integrations.
type appState struct {
	// BuildRevision is the vcs.revision of the binary that last successfully
	// ran ensureIntegrations. An empty value means no check has run yet.
	BuildRevision string `json:"build_revision"`
}

// defaultStatePath returns the path to state.json, respecting XDG_DATA_HOME.
// Mirrors the pattern used by history.DefaultHistoryPath and
// monitor.DefaultStatePath.
func defaultStatePath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop", "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		debug.Error("defaultStatePath: UserHomeDir: %v", err)
		return filepath.Join(".local", "share", "pop", "state.json")
	}
	return filepath.Join(home, ".local", "share", "pop", "state.json")
}

// loadAppState reads state.json. A missing or corrupt file is treated as an
// empty state, so the auto-updater re-checks everything on the next launch.
func loadAppState() *appState {
	path := defaultStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			debug.Error("loadAppState: %v", err)
		}
		return &appState{}
	}
	var s appState
	if err := json.Unmarshal(data, &s); err != nil {
		debug.Error("loadAppState: unmarshal: %v", err)
		return &appState{}
	}
	return &s
}

// saveAppState writes state.json, creating parent directories as needed.
func saveAppState(s *appState) error {
	path := defaultStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ----- Auto-update integrations ---------------------------------------------

// integrationAgents is the hardcoded list of agents that ensureIntegrations
// checks on each startup. Small enough that a registry is overkill; changes
// here must also update the integrateCmd ValidArgs list.
var integrationAgents = []string{"claude", "pi", "opencode"}

// ensureIntegrations checks whether installed agent integrations are stale
// against the currently running binary's VCS revision and updates any that
// need it. Returns warnings to surface in the picker for any failures.
//
// Behavior:
//   - "dev" builds (no VCS revision) are skipped entirely — nothing to track.
//   - state.json is read once; if its revision matches the current binary,
//     nothing runs (the common fast path after first check on a given build).
//   - For each agent, a dry-run install reports whether it's installed and
//     whether the current bytes match the embedded ones. Installed+stale
//     triggers a real install; anything else is a no-op.
//   - state.json is stamped with the current revision only when every stale
//     agent updated successfully. Partial failures leave the old revision
//     in place so the next launch retries.
//   - Warnings are returned only for agents that are demonstrably installed
//     but failed to check or update. Non-installed agents are silent.
func ensureIntegrations() []string {
	return ensureIntegrationsForRevision(buildRevision())
}

// integrationUpdateResult reports what updateStaleIntegrations did during
// one pass: which agents were actually refreshed (Updated) and which
// warnings should be surfaced (Warnings).
type integrationUpdateResult struct {
	Updated  []string
	Warnings []string
}

// updateStaleIntegrations is the pure core of the auto-update flow. For each
// configured agent it runs a dry-run install (via newDry) to detect whether
// the agent is installed and whether its content is stale, and — if both
// are true — runs a real install (via newReal) to refresh it.
//
// The function does not read or write state.json, does not gate on the
// binary revision, and does not emit output. Callers layer those behaviors
// on top (see ensureIntegrationsForRevisionWith and runIntegrateUpdateExistingWith).
func updateStaleIntegrations(newDry, newReal func() *integrateDeps) integrationUpdateResult {
	var result integrationUpdateResult

	for _, agent := range integrationAgents {
		// Dry-run first to determine installed/changed state.
		dryDeps := newDry()
		if err := runIntegrateWith(dryDeps, agent); err != nil {
			debug.Error("updateStaleIntegrations: dry-run %s: %v", agent, err)
			// Only warn if the agent is known to be installed — errors
			// for uninstalled agents are noise.
			if dryDeps.installed {
				result.Warnings = append(result.Warnings, fmt.Sprintf("failed to check %s integration: %v", agent, err))
			}
			continue
		}

		if !dryDeps.installed || !dryDeps.changed {
			continue // not installed, or already up to date
		}

		// Agent is installed and stale — do the real install.
		realDeps := newReal()
		realDeps.stdout = nil // auto-update runs silently on success
		if err := runIntegrateWith(realDeps, agent); err != nil {
			debug.Error("updateStaleIntegrations: update %s: %v", agent, err)
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to update %s integration (see pop.log)", agent))
			continue
		}

		result.Updated = append(result.Updated, agent)
		debug.Log("updateStaleIntegrations: updated %s integration", agent)
	}

	return result
}

// stampRevisionIfSuccess writes the given revision to state.json, but only
// when no warnings were produced. Partial failures deliberately leave the
// previous revision in place so the next launch retries. A "dev" revision
// is never stamped — dev builds have no stable staleness marker.
func stampRevisionIfSuccess(rev string, result integrationUpdateResult) {
	if len(result.Warnings) > 0 || rev == "dev" {
		return
	}
	state := loadAppState()
	if state.BuildRevision == rev {
		return // already stamped
	}
	state.BuildRevision = rev
	if err := saveAppState(state); err != nil {
		debug.Error("stampRevisionIfSuccess: save state: %v", err)
	}
}

// ensureIntegrationsForRevision is the testable core of ensureIntegrations,
// parameterized by the binary revision so tests can drive it directly without
// having to stamp a VCS revision into the test binary.
func ensureIntegrationsForRevision(rev string) []string {
	return ensureIntegrationsForRevisionWith(rev, dryRunIntegrateDeps, defaultIntegrateDeps)
}

// ensureIntegrationsForRevisionWith is the fully testable variant: callers
// provide factories that construct dry-run and real deps. Tests use this to
// share a fake filesystem between the check and update phases and to inject
// write failures for the retry-semantics cases.
func ensureIntegrationsForRevisionWith(rev string, newDry, newReal func() *integrateDeps) []string {
	if rev == "dev" {
		return nil // nothing to track
	}
	state := loadAppState()
	if state.BuildRevision == rev {
		return nil // already checked this binary
	}

	result := updateStaleIntegrations(newDry, newReal)
	stampRevisionIfSuccess(rev, result)
	return result.Warnings
}

// runIntegrateUpdateExisting is the CLI entry point behind
// `pop integrate --update-existing`. Unlike the runtime auto-update
// (ensureIntegrations), it always runs regardless of state.json and prints
// its outcome to stdout/stderr so install-time hooks can surface the result
// in the user's shell.
//
// Invariants:
//   - Silent on no-op (no installed agents, or everything already current).
//   - One "✓ Updated <agent> integration" line per agent that was refreshed.
//   - One "⚠ ..." line per warning, written to stderr.
//   - On full success, state.json is stamped with the current revision so
//     the runtime auto-update short-circuits on subsequent launches.
//   - Always exits 0. Non-fatal by design: a broken integration must not
//     block `make install` or `brew install` from completing.
func runIntegrateUpdateExisting() error {
	return runIntegrateUpdateExistingWith(
		buildRevision(),
		dryRunIntegrateDeps,
		defaultIntegrateDeps,
		os.Stdout,
		os.Stderr,
	)
}

// runIntegrateUpdateExistingWith is the fully testable variant. Tests inject
// fake deps factories, a fixed revision, and bytes.Buffer writers for output.
func runIntegrateUpdateExistingWith(
	rev string,
	newDry, newReal func() *integrateDeps,
	stdout, stderr io.Writer,
) error {
	result := updateStaleIntegrations(newDry, newReal)

	for _, agent := range result.Updated {
		fmt.Fprintf(stdout, "✓ Updated %s integration\n", agent)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "⚠ %s\n", w)
	}

	stampRevisionIfSuccess(rev, result)
	return nil
}
