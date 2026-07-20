package cmd

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// renderComponent is the render engine: a pure function from (component, agent)
// to a rendered file tree, keyed by path relative to the component's render
// root. It operates entirely on embedded content (skillFiles) and performs no
// I/O — the link installer is responsible for writing the tree to disk and
// creating symlinks at the agent's location.
//
// Every per-agent transform lives here: frontmatter name injection and the
// skill-directory layout. Agents that cannot host the component (per the
// catalog support matrix) return an error rather than a degraded tree.
// The installed name of each skill is `<prefix><base>`; prefix is the resolved
// skills_prefix (default `pop-`, empty for bare names — ADR 0063). The same
// prefix must reach the render-tree names, the agent-location link names, and
// conflict detection, so it is resolved once by the caller and threaded in.
func renderComponent(id ComponentID, agent, prefix string) (map[string][]byte, error) {
	comp, ok := lookupComponent(id)
	if !ok {
		return nil, fmt.Errorf("unknown component %q", id)
	}
	agent = strings.ToLower(agent)
	if !comp.supported(agent) {
		return nil, fmt.Errorf("component %q is not supported for agent %q", id, agent)
	}

	switch id {
	case ComponentPaneSkill, ComponentTaskSkills:
		return renderSkillComponent(comp, agent, prefix)
	default:
		return nil, fmt.Errorf("component %q has no file-based render", id)
	}
}

// renderSkillComponent renders each of the component's embedded skill sources
// into the agent's skill layout. A source is one of two shapes:
//
//   - A single `.md` file `skills/pop/<base>.md` — a one-file skill named
//     `<prefix><base>` (the pane skill).
//   - A directory `skills/pop/<base>` holding `SKILL.md` plus any companion
//     documents — a multi-file skill named `<prefix><base>`. The companion
//     files ride alongside the skill body so the body's relative references
//     resolve (grill-with-docs and its two format documents).
func renderSkillComponent(comp integrationComponent, agent, prefix string) (map[string][]byte, error) {
	baseNames := fileBasedSkillBaseNames()
	tree := make(map[string][]byte, len(comp.sources))
	for _, src := range comp.sources {
		if strings.HasSuffix(src, ".md") {
			if err := renderSingleFileSkill(tree, agent, prefix, src, baseNames); err != nil {
				return nil, err
			}
			continue
		}
		if err := renderMultiFileSkill(tree, agent, prefix, src, baseNames); err != nil {
			return nil, err
		}
	}
	return tree, nil
}

// renderSingleFileSkill renders a one-file skill source into the agent's layout.
func renderSingleFileSkill(tree map[string][]byte, agent, prefix, src string, baseNames []string) error {
	data, err := skillFiles.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read embedded skill %s: %w", src, err)
	}
	skillName := prefix + strings.TrimSuffix(filepath.Base(src), ".md")
	content := rewriteSkillReferences(string(data), prefix, baseNames)
	rel, rendered, err := renderSkillFile(agent, skillName, content)
	if err != nil {
		return err
	}
	tree[rel] = []byte(rendered)
	return nil
}

// renderMultiFileSkill renders a directory-shaped skill source: its `SKILL.md`
// becomes the skill body (with the frontmatter name injected) and every other
// file is emitted verbatim alongside it under `<prefix><base>/`. Directory-hosting
// agents (claude, codex, pi, cursor, opencode) preserve companion documents
// so relative references in the skill body resolve.
func renderMultiFileSkill(tree map[string][]byte, agent, prefix, dir string, baseNames []string) error {
	skillName := prefix + path.Base(dir)
	switch agent {
	case "claude", "codex", "pi", "cursor", "opencode":
	default:
		return fmt.Errorf("agent %q cannot host multi-file skill %q (no skill-directory layout)", agent, skillName)
	}
	entries, err := fs.ReadDir(skillFiles, dir)
	if err != nil {
		return fmt.Errorf("failed to read embedded skill dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := skillFiles.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("failed to read embedded skill file %s/%s: %w", dir, e.Name(), err)
		}
		content := rewriteSkillReferences(string(data), prefix, baseNames)
		if e.Name() == "SKILL.md" {
			tree[skillName+"/SKILL.md"] = []byte(injectOwnershipMarker(injectFrontmatterName(content, skillName)))
			continue
		}
		tree[skillName+"/"+e.Name()] = []byte(content)
	}
	return nil
}

// renderSkillFile returns the relative path and rendered bytes for a single
// skill under the given agent's layout.
//
// claude, codex, pi, and cursor host skills as directories: `<skillName>/SKILL.md`
// with the frontmatter `name` injected so the body matches the directory name.
//
// opencode hosts skills as a flat single file `<skillName>.md` — it has no
// skill-directory layout, so the name is not injected (the file name itself
// carries the identity), but the ownership marker still is.
//
// Every rendered skill carries the name-independent `pop-owned: true` marker so
// ownership is decided by the marker rather than the skill name (ADR 0011,
// ADR 0063).
func renderSkillFile(agent, skillName, content string) (rel, rendered string, err error) {
	switch agent {
	case "claude", "codex", "pi", "cursor":
		return skillName + "/SKILL.md", injectOwnershipMarker(injectFrontmatterName(content, skillName)), nil
	case "opencode":
		return skillName + ".md", injectOwnershipMarker(content), nil
	default:
		return "", "", fmt.Errorf("agent %q has no skill render layout", agent)
	}
}

// fileBasedSkillBaseNames returns the base name of every embedded skill source
// listed in the integration catalog, longest first so shorter names cannot
// partially match inside longer hyphenated names during rewrite.
func fileBasedSkillBaseNames() []string {
	seen := make(map[string]struct{})
	var names []string
	for _, c := range integrationCatalog {
		for _, src := range c.sources {
			base := skillBaseNameFromSource(src)
			if _, ok := seen[base]; ok {
				continue
			}
			seen[base] = struct{}{}
			names = append(names, base)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})
	return names
}

func skillBaseNameFromSource(src string) string {
	if strings.HasSuffix(src, ".md") {
		return strings.TrimSuffix(filepath.Base(src), ".md")
	}
	return path.Base(src)
}

// rewriteSkillReferences rewrites cross-skill references in rendered bodies and
// companion documents to their resolved installed names (<prefix><base>). An
// empty prefix is a no-op — embedded sources already carry bare base names.
//
// Only reference-shaped occurrences are rewritten: backticked names, slash
// invocations, "the <name> skill", and (for hyphenated names only) bare
// word-boundary hits. Frontmatter and HTML attribution comments are protected
// so descriptions and upstream drift pointers stay intact. Common-word skill
// names like research and prototype are limited to those reference shapes so
// ticket-type vocabulary and ordinary prose are not corrupted.
func rewriteSkillReferences(content, prefix string, baseNames []string) string {
	if prefix == "" || len(baseNames) == 0 {
		return content
	}
	protected, placeholders := protectRewriteRegions(content)
	for _, base := range baseNames {
		protected = rewriteSkillReference(protected, prefix, base)
	}
	return restoreRewriteRegions(protected, placeholders)
}

const rewriteProtectSentinel = "\x00POP_REWRITE_PROTECT_"

func protectRewriteRegions(content string) (string, []string) {
	var placeholders []string
	if strings.HasPrefix(content, "---\n") {
		if end := strings.Index(content[4:], "\n---"); end >= 0 {
			end += 4
			closing := end
			if strings.HasPrefix(content[end:], "\n---\n") {
				closing = end + 5
			} else if strings.HasPrefix(content[end:], "\n---") {
				closing = end + 4
			}
			placeholders = append(placeholders, content[:closing])
			content = rewriteProtectPlaceholder(len(placeholders)-1) + content[closing:]
		}
	}
	commentRE := regexp.MustCompile(`<!--[\s\S]*?-->`)
	content = commentRE.ReplaceAllStringFunc(content, func(match string) string {
		placeholders = append(placeholders, match)
		return rewriteProtectPlaceholder(len(placeholders) - 1)
	})
	return content, placeholders
}

func rewriteProtectPlaceholder(i int) string {
	return rewriteProtectSentinel + fmt.Sprintf("%d", i) + "\x00"
}

func restoreRewriteRegions(content string, placeholders []string) string {
	for i, ph := range placeholders {
		content = strings.Replace(content, rewriteProtectPlaceholder(i), ph, 1)
	}
	return content
}

func rewriteSkillReference(content, prefix, base string) string {
	rewritten := prefix + base
	if strings.Contains(base, "-") {
		return rewriteHyphenatedSkillReference(content, rewritten, base)
	}
	return rewriteAmbiguousSkillReference(content, rewritten, base)
}

func rewriteHyphenatedSkillReference(content, rewritten, base string) string {
	content = rewriteBacktickSkillReference(content, rewritten, base)
	content = rewriteSlashSkillReference(content, rewritten, base)
	content = rewriteTheSkillReference(content, rewritten, base)
	content = rewriteBareHyphenatedReference(content, rewritten, base)
	return content
}

func rewriteAmbiguousSkillReference(content, rewritten, base string) string {
	content = rewriteSlashSkillReference(content, rewritten, base)
	content = rewriteTheSkillReference(content, rewritten, base)
	content = rewriteInvocationBacktickReferences(content, rewritten, base)
	return content
}

func rewriteBacktickSkillReference(content, rewritten, base string) string {
	old := "`" + base + "`"
	if strings.Contains(content, old) {
		content = strings.ReplaceAll(content, old, "`"+rewritten+"`")
	}
	return content
}

// rewriteInvocationBacktickReferences rewrites backticked skill names that
// appear only in cross-skill invocation contexts. Bare backticks on common-word
// names (ticket types, branch paths) are left alone.
func rewriteInvocationBacktickReferences(content, rewritten, base string) string {
	q := "`" + base + "`"
	rq := "`" + rewritten + "`"
	for _, pat := range []string{
		"run the " + q + " skill",
		"tickets use " + q,
		"with " + q,
		"suggest " + q,
		"when " + q + " writes",
		"use the " + q + " skill",
	} {
		content = strings.ReplaceAll(content, pat, strings.Replace(pat, q, rq, 1))
	}
	return content
}

func rewriteSlashSkillReference(content, rewritten, base string) string {
	needle := "/" + base
	var b strings.Builder
	b.Grow(len(content))
	for i := 0; i < len(content); {
		j := strings.Index(content[i:], needle)
		if j < 0 {
			b.WriteString(content[i:])
			break
		}
		j += i
		if j > 0 && isSkillRefWordChar(content[j-1]) {
			b.WriteString(content[i : j+len(needle)])
			i = j + len(needle)
			continue
		}
		nextIdx := j + len(needle)
		if nextIdx < len(content) {
			next := content[nextIdx]
			if next == '/' || next == '<' {
				b.WriteString(content[i:nextIdx])
				i = nextIdx
				continue
			}
			if isSkillRefWordChar(next) {
				b.WriteString(content[i:nextIdx])
				i = nextIdx
				continue
			}
		}
		b.WriteString(content[i:j])
		b.WriteString("/")
		b.WriteString(rewritten)
		i = nextIdx
	}
	return b.String()
}

func isSkillRefWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}

func rewriteTheSkillReference(content, rewritten, base string) string {
	pat := `((?:^|[^a-zA-Z0-9_-]))` + regexp.QuoteMeta(base) + ` skill((?:$|[^a-zA-Z0-9_-]))`
	re := regexp.MustCompile(pat)
	return re.ReplaceAllString(content, "${1}"+rewritten+" skill${2}")
}

func rewriteBareHyphenatedReference(content, rewritten, base string) string {
	pat := `((?:^|[^a-zA-Z0-9_-]))` + regexp.QuoteMeta(base) + `((?:$|[^a-zA-Z0-9_-]))`
	re := regexp.MustCompile(pat)
	return re.ReplaceAllString(content, "${1}"+rewritten+"${2}")
}
