package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// checkedInAgentHome holds the files every agent session starts from: a home
// with no plugins, hooks, MCP servers or memory. The digest of these files is
// what a Trial and a grade record about the home they ran in.
//
//go:embed agent-home
var checkedInAgentHome embed.FS

const checkedInAgentHomeRoot = "agent-home"

// agentHomeMarker names the file that says the harness made a directory. The
// clear removes files, so it only clears a directory that holds this marker.
const agentHomeMarker = ".pop-eval-agent-home"

// homeAgent is one agent CLI that the agent home isolates: its directory under
// the root, the variable that points the CLI at that directory, and how a human
// logs the CLI in there.
type homeAgent struct {
	preset      string
	dir         string
	envKey      string
	login       string
	passThrough []string
}

var homeAgents = []homeAgent{
	{preset: "claude", dir: "claude", envKey: "CLAUDE_CONFIG_DIR", login: "claude auth login", passThrough: []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"}},
	{preset: "codex", dir: "codex", envKey: "CODEX_HOME", login: "codex login"},
}

// protectedHomeEntries are the entries the clear never removes: the logins a
// human made into the home, and the Claude account state that goes with a
// login. The harness never reads, writes or copies them.
var protectedHomeEntries = []string{".credentials.json", ".claude.json", "auth.json"}

// agentHome is the pinned configuration home of every agent the harness
// starts, in place of the human's own ~/.claude and ~/.codex. The repository's
// own AGENTS.md and CLAUDE.md are in the clone, so the agent still reads them.
type agentHome struct {
	root string
}

func newAgentHome(root string) (agentHome, error) {
	if strings.TrimSpace(root) == "" {
		return agentHome{}, errors.New("agent home root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return agentHome{}, fmt.Errorf("resolve agent home root: %w", err)
	}
	return agentHome{root: abs}, nil
}

func agentHomeFlag(flags *flag.FlagSet) *string {
	return flags.String("agent-home", defaultAgentHomeRoot, "Pinned agent home that every agent session runs in")
}

func (h agentHome) dir(agent homeAgent) string {
	return filepath.Join(h.root, agent.dir)
}

// env points each isolated agent CLI at its directory in the home.
func (h agentHome) env() []string {
	entries := make([]string, 0, len(homeAgents))
	for _, agent := range homeAgents {
		entries = append(entries, agent.envKey+"="+h.dir(agent))
	}
	return entries
}

// prepare puts the home in the state every agent session starts from: each
// agent directory holds only its logins and the checked-in files. Session
// state from an earlier session, such as transcripts, todos and memory, is
// removed.
func (h agentHome) prepare() error {
	if h.root == "" {
		return errors.New("agent home root is empty")
	}
	for _, agent := range homeAgents {
		if err := clearAgentDir(h.dir(agent)); err != nil {
			return err
		}
	}
	return fs.WalkDir(checkedInAgentHome, checkedInAgentHomeRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := checkedInAgentHome.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(h.root, filepath.FromSlash(strings.TrimPrefix(path, checkedInAgentHomeRoot+"/")))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return tasks.WriteAtomic(target, data, 0o600)
	})
}

// clearAgentDir removes every entry of dir except the protected ones. A
// directory that the harness did not make, such as the human's own ~/.claude,
// is refused before anything is removed.
func clearAgentDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(entries) == 0) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create agent home %s: %w", dir, err)
		}
		return os.WriteFile(filepath.Join(dir, agentHomeMarker), nil, 0o600)
	}
	if err != nil {
		return fmt.Errorf("read agent home %s: %w", dir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, agentHomeMarker)); err != nil {
		return fmt.Errorf("agent home %s was not made by the harness (it has no %s), so the harness does not clear it; select an empty or absent directory with --agent-home", dir, agentHomeMarker)
	}
	for _, entry := range entries {
		if entry.Name() == agentHomeMarker || slices.Contains(protectedHomeEntries, entry.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("clear agent home %s: %w", dir, err)
		}
	}
	return nil
}

// agentHomeDigest names the checked-in home files. A Trial or a grade with
// another digest ran in a home with other settings.
func agentHomeDigest() string {
	hash := sha256.New()
	err := fs.WalkDir(checkedInAgentHome, checkedInAgentHomeRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := checkedInAgentHome.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", path, len(data))
		hash.Write(data)
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("read embedded agent home: %v", err))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// deps gives pop's capture seam a view where every agent command runs with the
// home's variables, and where pop reads an agent's files, such as a Codex
// rollout, from the home.
func (h agentHome) deps() *tasks.Deps {
	d := tasks.DefaultDeps()
	values := map[string]string{}
	for _, agent := range homeAgents {
		values[agent.envKey] = h.dir(agent)
	}
	d.FS = envFS{FileSystem: d.FS, values: values}
	d.Runner = homeRunner{env: h.env()}
	return d
}

// capture prepares the home, then runs one captured agent session in it.
func (h agentHome) capture(opts tasks.CapturedAgentOptions) (*tasks.CapturedAgentAttempt, error) {
	if err := h.prepare(); err != nil {
		return nil, fmt.Errorf("prepare agent home: %w", err)
	}
	return tasks.RunCapturedAgentInvocation(h.deps(), opts)
}

// preflight prepares the home and refuses it when an agent preset cannot run
// there: the home does not isolate the preset, or the preset's own auth status
// command says it has no login. The status commands read only local state, so
// the check spends no model tokens.
func (h agentHome) preflight(presets []string) error {
	if err := h.prepare(); err != nil {
		return fmt.Errorf("preflight agent home: %w", err)
	}
	d := h.deps()
	var problems []string
	seen := map[string]bool{}
	for _, preset := range presets {
		if seen[preset] {
			continue
		}
		seen[preset] = true
		index := slices.IndexFunc(homeAgents, func(agent homeAgent) bool { return agent.preset == preset })
		if index < 0 {
			problems = append(problems, fmt.Sprintf("agent %q has no directory in the agent home, so it would run with your own configuration; the agent home isolates only claude and codex", preset))
			continue
		}
		agent := homeAgents[index]
		if probe := tasks.ProbeAgentAuthentication(d, h.root, preset); probe.Status != tasks.AgentAuthAuthenticated {
			problems = append(problems, agent.loginAdvice(h.dir(agent), probe.Detail))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("preflight agent home %s:\n  %s", h.root, strings.Join(problems, "\n  "))
	}
	return nil
}

func (a homeAgent) loginAdvice(dir, detail string) string {
	advice := fmt.Sprintf("%s is not logged in at %s (%s). Log in once with: %s=%s %s", a.preset, dir, detail, a.envKey, shellQuote(dir), a.login)
	if len(a.passThrough) > 0 {
		advice += ". Or set " + strings.Join(a.passThrough, " or ") + ", for example with op-cache run"
	}
	return advice
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// envFS answers selected environment reads with fixed values, so pop resolves
// those paths inside the harness's own directories.
type envFS struct {
	deps.FileSystem
	values map[string]string
}

func (f envFS) Getenv(key string) string {
	if value, ok := f.values[key]; ok {
		return value
	}
	return f.FileSystem.Getenv(key)
}

// homeRunner starts every command with the agent home's variables over the
// harness's own environment. An agent invocation's own entries come after
// them.
type homeRunner struct {
	env []string
}

func (r homeRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r homeRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return r.StartWithEnv(ctx, dir, nil, stdout, stderr, name, args...)
}

func (r homeRunner) StartWithEnv(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return tasks.RealCommandRunner{}.StartWithEnv(ctx, dir, append(slices.Clone(r.env), env...), stdout, stderr, name, args...)
}

func runAgentHomeCommand(args []string) error {
	flags := flag.NewFlagSet("agent-home", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := agentHomeFlag(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: go run ./eval agent-home [--agent-home <dir>]")
	}
	home, err := newAgentHome(*root)
	if err != nil {
		return err
	}
	presets := make([]string, 0, len(homeAgents))
	for _, agent := range homeAgents {
		presets = append(presets, agent.preset)
	}
	if err := home.preflight(presets); err != nil {
		return err
	}
	fmt.Printf("Agent home %s is ready: %s logged in\n", home.root, strings.Join(presets, " and "))
	return nil
}
