package cmd

import (
	"fmt"
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
	case ComponentPaneSkill, ComponentWorkloadSkills:
		return renderSkillComponent(comp, agent)
	default:
		return nil, fmt.Errorf("component %q has no file-based render", id)
	}
}

// renderSkillComponent renders each of the component's embedded skill sources
// into the agent's skill layout. Each source `skills/pop/<base>.md` becomes a
// skill named `pop-<base>`.
func renderSkillComponent(comp integrationComponent, agent string) (map[string][]byte, error) {
	tree := make(map[string][]byte, len(comp.sources))
	for _, src := range comp.sources {
		data, err := skillFiles.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded skill %s: %w", src, err)
		}
		base := strings.TrimSuffix(filepath.Base(src), ".md")
		skillName := "pop-" + base
		rel, content, err := renderSkillFile(agent, skillName, string(data))
		if err != nil {
			return nil, err
		}
		tree[rel] = []byte(content)
	}
	return tree, nil
}

// renderSkillFile returns the relative path and rendered bytes for a single
// skill under the given agent's layout.
//
// claude, pi, and cursor host skills as directories: `<skillName>/SKILL.md`
// with the frontmatter `name` injected so the body matches the directory name.
//
// opencode hosts skills as a flat single file `<skillName>.md` — it has no
// skill-directory layout, so the content is emitted verbatim (no name
// injection; the file name itself carries the identity).
func renderSkillFile(agent, skillName, content string) (rel, rendered string, err error) {
	switch agent {
	case "claude", "pi", "cursor":
		return skillName + "/SKILL.md", injectFrontmatterName(content, skillName), nil
	case "opencode":
		return skillName + ".md", content, nil
	default:
		return "", "", fmt.Errorf("agent %q has no skill render layout", agent)
	}
}
