package cmd

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
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
func renderComponent(id ComponentID, agent string) (map[string][]byte, error) {
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
		return renderSkillComponent(comp, agent)
	default:
		return nil, fmt.Errorf("component %q has no file-based render", id)
	}
}

// renderSkillComponent renders each of the component's embedded skill sources
// into the agent's skill layout. A source is one of two shapes:
//
//   - A single `.md` file `skills/pop/<base>.md` — a one-file skill named
//     `pop-<base>` (the pane skill).
//   - A directory `skills/pop/<base>` holding `SKILL.md` plus any companion
//     documents — a multi-file skill named `pop-<base>`. The companion files
//     ride alongside the skill body so the body's relative references resolve
//     (grill-with-docs and its two format documents).
func renderSkillComponent(comp integrationComponent, agent string) (map[string][]byte, error) {
	tree := make(map[string][]byte, len(comp.sources))
	for _, src := range comp.sources {
		if strings.HasSuffix(src, ".md") {
			if err := renderSingleFileSkill(tree, agent, src); err != nil {
				return nil, err
			}
			continue
		}
		if err := renderMultiFileSkill(tree, agent, src); err != nil {
			return nil, err
		}
	}
	return tree, nil
}

// renderSingleFileSkill renders a one-file skill source into the agent's layout.
func renderSingleFileSkill(tree map[string][]byte, agent, src string) error {
	data, err := skillFiles.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read embedded skill %s: %w", src, err)
	}
	skillName := "pop-" + strings.TrimSuffix(filepath.Base(src), ".md")
	rel, content, err := renderSkillFile(agent, skillName, string(data))
	if err != nil {
		return err
	}
	tree[rel] = []byte(content)
	return nil
}

// renderMultiFileSkill renders a directory-shaped skill source: its `SKILL.md`
// becomes the skill body (with the frontmatter name injected) and every other
// file is emitted verbatim alongside it under `pop-<base>/`. Only the
// directory-hosting agents (claude, pi, cursor) can host a multi-file skill; an
// agent with a flat skill layout (opencode) is rejected rather than given a
// skill whose companion files would be lost. The catalog support matrix keeps
// this branch from being reached for an unsupported pair.
func renderMultiFileSkill(tree map[string][]byte, agent, dir string) error {
	skillName := "pop-" + path.Base(dir)
	switch agent {
	case "claude", "pi", "cursor":
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
		if e.Name() == "SKILL.md" {
			tree[skillName+"/SKILL.md"] = []byte(injectOwnershipMarker(injectFrontmatterName(string(data), skillName)))
			continue
		}
		// Companion document — rides alongside the body, byte-for-byte.
		tree[skillName+"/"+e.Name()] = data
	}
	return nil
}

// renderSkillFile returns the relative path and rendered bytes for a single
// skill under the given agent's layout.
//
// claude, pi, and cursor host skills as directories: `<skillName>/SKILL.md`
// with the frontmatter `name` injected so the body matches the directory name.
//
// opencode hosts skills as a flat single file `<skillName>.md` — it has no
// skill-directory layout, so the name is not injected (the file name itself
// carries the identity), but the ownership marker still is.
//
// Every rendered skill carries the name-independent `pop-owned: true` marker so
// ownership is decided by the marker rather than the skill name (ADR 0011,
// skill-prefix slice 02).
func renderSkillFile(agent, skillName, content string) (rel, rendered string, err error) {
	switch agent {
	case "claude", "pi", "cursor":
		return skillName + "/SKILL.md", injectOwnershipMarker(injectFrontmatterName(content, skillName)), nil
	case "opencode":
		return skillName + ".md", injectOwnershipMarker(content), nil
	default:
		return "", "", fmt.Errorf("agent %q has no skill render layout", agent)
	}
}
