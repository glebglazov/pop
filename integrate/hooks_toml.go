package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ----- Kimi integration ------------------------------------------------------

// kimiPopHooks defines the hook commands merged into kimi-code's config.toml as
// [[hooks]] array-of-tables entries. kimi spawns each command through a shell
// with the hook payload on stdin, so the command strings are shaped exactly like
// claude's and codex's.
//
// The event names are kimi's own vocabulary. "working" rides PostToolUse rather
// than PreToolUse because kimi's PreToolUse hook is a blocking veto point that
// every tool call waits on — status reporting has no business sitting there.
var kimiPopHooks = []hookSpec{
	{"SessionStart", "pop pane set-status clear 2>/dev/null || true"},
	{"SessionStart", "pop pane set-topic --clear 2>/dev/null || true"},
	{"PostToolUse", "pop pane set-status working 2>/dev/null || true"},
	{"Notification", "pop pane set-status unread 2>/dev/null || true"},
	{"Stop", "pop pane set-status unread 2>/dev/null || true"},
}

// kimiHome resolves kimi-code's data root the way the kimi CLI does:
// $KIMI_CODE_HOME when set, else ~/.kimi-code. Both the config.toml that carries
// the status wiring and the skills/ directory that hosts file-based components
// hang off it, so a relocated kimi home moves pop's whole integration with it.
func kimiHome(d *Deps, home string) string {
	if v := strings.TrimSpace(getenv(d, "KIMI_CODE_HOME")); v != "" {
		return v
	}
	return filepath.Join(home, ".kimi-code")
}

// tomlPopHookMarker heads the run of pop-owned [[hooks]] blocks so a human
// reading config.toml knows the region is generated. Ownership itself is never
// decided by the marker — a pop-owned block is one whose command is a pop
// status-report command (isPopHookCommand), the same test the JSON dialects use
// — so a user who deletes the comment does not strand a block.
const tomlPopHookMarker = "# pop status wiring — managed by 'pop integrate kimi'; replaced on update"

// installTOMLHooks merges pop's hook entries into a TOML config file as
// [[hooks]] blocks, preserving every hand-authored byte around them.
//
// The merge is textual, not a decode-and-re-encode: kimi's config.toml is a file
// the user hand-writes (providers, model aliases, comments, key order), and no
// Go TOML encoder can round-trip comments or ordering. Previously installed pop
// blocks are cut out and the current set appended, so re-running is idempotent
// down to the byte and an old hook set is pruned rather than duplicated.
func installTOMLHooks(r *run, path string, hooksToInstall []hookSpec) error {
	d := r.deps

	existing := ""
	data, err := d.readFile(path)
	if err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	kept, hadPopHooks := stripPopHookBlocks(existing)
	merged := renderTOMLHookBlocks(hooksToInstall)
	if kept != "" {
		merged = kept + "\n\n" + merged
	}

	if err := d.mkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if err := d.writeFile(path, []byte(merged), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	// Dry-run "installed" detection: kimi writes config.toml itself on first
	// launch, so file presence — which the dry-run write seam reads as installed
	// — says nothing about pop. Pop-owned blocks are the only honest signal, and
	// they could only have been written by a previous integrate run.
	if r.dryRun {
		r.installed = hadPopHooks
	}

	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed %d hook(s) in %s\n", len(hooksToInstall), path)
	}
	return nil
}

// stripTOMLHooks removes pop's [[hooks]] blocks from a TOML config file, leaving
// every other table, key, and comment exactly as the user wrote it. A missing
// file, or one carrying no pop blocks, is reported as nothing-to-remove and
// left untouched.
func stripTOMLHooks(d *Deps, path string) error {
	data, err := d.readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if d.stdout != nil {
				fmt.Fprintf(d.stdout, "no pop hooks in %s — nothing to remove\n", path)
			}
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	kept, hadPopHooks := stripPopHookBlocks(string(data))
	if !hadPopHooks {
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "no pop hooks in %s — nothing to remove\n", path)
		}
		return nil
	}

	out := ""
	if kept != "" {
		out = kept + "\n"
	}
	if err := d.writeFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Removed pop hooks from %s\n", path)
	}
	return nil
}

// tomlHasPopHooks reports whether the TOML config file carries any pop-owned
// [[hooks]] block.
func tomlHasPopHooks(d *Deps, path string) (bool, error) {
	data, err := d.readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	_, hadPopHooks := stripPopHookBlocks(string(data))
	return hadPopHooks, nil
}

// renderTOMLHookBlocks renders pop's hook set as [[hooks]] blocks, headed by the
// ownership marker comment. Appending at end-of-file is always valid for an
// array of tables: a table header closes whatever table preceded it.
func renderTOMLHookBlocks(hooks []hookSpec) string {
	var b strings.Builder
	b.WriteString(tomlPopHookMarker)
	b.WriteString("\n")
	for _, h := range hooks {
		b.WriteString("\n[[hooks]]\n")
		fmt.Fprintf(&b, "event = %s\n", tomlQuote(h.event))
		fmt.Fprintf(&b, "command = %s\n", tomlQuote(h.command))
	}
	return b.String()
}

// stripPopHookBlocks removes every pop-owned [[hooks]] block and every pop
// marker comment from TOML text, returning what is left (with trailing blank
// lines trimmed) and whether anything pop-owned was found. Nothing else is
// touched — a user's own [[hooks]] entry, unrelated tables, comments, and key
// order all survive verbatim.
//
// A removed block reaches exactly as far as pop's own writes could: the header
// plus the run of hook-key assignments and blank lines under it. The first line
// that is not one of those ends the block, so a comment or a top-level key that
// happens to sit after a pop block is never swallowed.
func stripPopHookBlocks(text string) (kept string, found bool) {
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var out []string
	inMultiline := ""
	for i := 0; i < len(lines); {
		line := lines[i]
		if inMultiline != "" {
			out = append(out, line)
			if strings.Contains(line, inMultiline) {
				inMultiline = ""
			}
			i++
			continue
		}
		if header, ok := tomlTableHeader(line); ok && header == "[[hooks]]" {
			end, command := scanTOMLHookBlock(lines, i)
			if isPopHookCommand(command) {
				found = true
				// Formatting pop added ahead of its own block goes with it, so
				// repeated install/remove cycles cannot accrete blank lines.
				for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				i = end
				continue
			}
			out = append(out, lines[i:end]...)
			i = end
			continue
		}
		if strings.TrimSpace(line) == tomlPopHookMarker {
			found = true
			i++
			continue
		}
		out = append(out, line)
		inMultiline = openMultilineDelimiter(line)
		i++
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n \t"), found
}

// scanTOMLHookBlock walks one [[hooks]] entry starting at its header line and
// returns the index just past it plus the entry's command value. The body is the
// run of hook-key assignments (kimi's schema admits only event, matcher,
// command, and timeout) and blank lines beneath the header.
func scanTOMLHookBlock(lines []string, start int) (end int, command string) {
	hookKeys := map[string]bool{"event": true, "matcher": true, "command": true, "timeout": true}
	end = start + 1
	for end < len(lines) {
		t := strings.TrimSpace(lines[end])
		if t == "" {
			end++
			continue
		}
		// A multi-line value ends the block here: only the outer scan tracks
		// string state, so continuing would read the string body as TOML.
		if openMultilineDelimiter(lines[end]) != "" {
			break
		}
		key, value, ok := tomlStringAssignment(lines[end])
		if !ok {
			if k := tomlAssignmentKey(lines[end]); k != "" && hookKeys[k] {
				end++
				continue
			}
			break
		}
		if !hookKeys[key] {
			break
		}
		if key == "command" {
			command = value
		}
		end++
	}
	return end, command
}

// tomlAssignmentKey returns the key of a `key = …` line whose value pop does not
// need to read (a number, a bool), or "" when the line is not an assignment.
func tomlAssignmentKey(line string) string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "[") {
		return ""
	}
	eq := strings.Index(t, "=")
	if eq < 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(t[:eq]), `"'`)
}

// openMultilineDelimiter reports the delimiter a multi-line string opened on
// this line and left unclosed, so table-header detection never fires on a
// bracketed line inside a string value.
func openMultilineDelimiter(line string) string {
	for _, delim := range []string{`"""`, `'''`} {
		if strings.Count(line, delim)%2 == 1 {
			return delim
		}
	}
	return ""
}

// tomlTableHeader returns the normalized header of a table or array-of-tables
// line ("[[hooks]]", "[models.\"a/b\"]"), with insignificant whitespace removed.
func tomlTableHeader(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "[") {
		return "", false
	}
	if i := strings.LastIndex(t, "]"); i >= 0 {
		t = t[:i+1]
	}
	if !strings.HasSuffix(t, "]") {
		return "", false
	}
	if strings.HasPrefix(t, "[[") {
		return "[[" + strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "[["), "]]")) + "]]", true
	}
	return "[" + strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "["), "]")) + "]", true
}

// tomlStringAssignment parses a `key = "value"` line, handling basic and
// literal strings. Anything else (a non-string value, a header, a comment)
// reports not-ok — pop only needs to read hook commands back.
func tomlStringAssignment(line string) (key, value string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", "", false
	}
	eq := strings.Index(t, "=")
	if eq < 0 {
		return "", "", false
	}
	key = strings.Trim(strings.TrimSpace(t[:eq]), `"'`)
	rest := strings.TrimSpace(t[eq+1:])
	switch {
	case strings.HasPrefix(rest, `"`):
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
				continue
			}
			if rest[i] == '"' {
				unquoted, err := strconv.Unquote(rest[:i+1])
				if err != nil {
					return "", "", false
				}
				return key, unquoted, true
			}
		}
	case strings.HasPrefix(rest, `'`):
		if i := strings.Index(rest[1:], `'`); i >= 0 {
			return key, rest[1 : i+1], true
		}
	}
	return "", "", false
}

// tomlQuote renders a Go string as a TOML basic string. pop's own hook commands
// and event names are plain ASCII, so Go's quoting is TOML-compatible.
func tomlQuote(s string) string {
	return strconv.Quote(s)
}
