package cmd

import (
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
)

// The project dashboard's nested row model. Everything here is a pure function
// over rows — rows in, ordered rows out — so the shape can be exercised from
// fixtures instead of from real discovery, real history and real tmux, which is
// what drowned the prototype's first attempt.
//
// Nesting is display-only. A child row keeps its Path and its SessionName; only
// its rendered label loses the "<project>/" prefix its session name is built
// from, so no session is ever renamed and no path changes.

// projectGroupPathPrefix marks the parent row nested mode invents for a
// repository that has worktree rows but no row of its own — a bare repo, whose
// worktrees *are* its top-level rows today. The prefix keeps the row's picker key
// unique while saying, in the one field every action reads, that it names no
// checkout: it is a grouping header, not something to open.
const projectGroupPathPrefix = "group:"

// projectRowMeta is what the nested row builder needs to know about a row and
// its ui.Item does not carry: whether the row is a non-trunk worktree, and which
// repository it belongs to. Both come off the ExpandedProject the row was built
// from, so grouping costs no store read, no git fork and no extra filesystem
// call (ADR-0110).
type projectRowMeta struct {
	// IsWorktree marks a row as a non-trunk worktree — the rows that become the
	// nested level.
	IsWorktree bool
	// Repo is the grouping key: the repository's directory basename, which every
	// row of one repository carries alike — a configured checkout, a bare repo's
	// worktree, and a pop-managed worktree (whose repo key hands the basename
	// back without any lookup).
	Repo string
	// RepoLabel is the repository's display name, which is what a synthesized
	// parent row is named after. Empty for rows that were never given one (a
	// managed worktree), in which case Repo stands in.
	RepoLabel string
}

// projectRowMetaFor keys the row metadata by Item.Path, the identity the picker
// itself uses for a row.
func projectRowMetaFor(expanded []project.ExpandedProject) map[string]projectRowMeta {
	meta := make(map[string]projectRowMeta, len(expanded))
	for _, ep := range expanded {
		meta[ep.Path] = projectRowMeta{
			IsWorktree: ep.IsWorktree,
			Repo:       ep.ProjectName,
			RepoLabel:  ep.ProjectLabel,
		}
	}
	return meta
}

// projectRowNode is a top-level row and the child rows hanging under it. Only
// nested mode builds one.
type projectRowNode struct {
	Row      ui.Item
	Children []ui.Item
	// rank is the node's place in the incoming order — the position of the most
	// recent row folded into it. It is what makes a project sink to its most
	// recent child's recency.
	rank int
	// weakRank ranks a synthesized parent whose worktrees are all cold, so a bare
	// repository with no live session still has a place in the list.
	weakRank int
}

// buildProjectRows arranges the session-aware rows for display. Flat mode hands
// the rows straight back — every worktree, session or not, under its full
// "<project>/<worktree>" name, which is what makes a flat list legible. Nested
// mode returns a different row set, not a rearrangement of the same one: a
// project's live worktree sessions become its children and its session-less
// worktrees drop out, reachable by typing a query instead.
func buildProjectRows(items []ui.Item, meta map[string]projectRowMeta, display config.WorktreeDisplay, expanded map[string]bool) []ui.Item {
	if display != config.WorktreeDisplayNested {
		return items
	}
	return flattenProjectRows(nestProjectRows(items, meta), expanded)
}

// nestProjectRows folds worktree rows into their project's row, in the order the
// incoming rows already carry: they arrive from sortByUnifiedRecency, so reusing
// their positions is the same comparator and the same direction at both levels
// rather than a second ordering rule that could disagree with the first.
func nestProjectRows(items []ui.Item, meta map[string]projectRowMeta) []projectRowNode {
	// A repository whose basename resolves to more than one project row cannot be
	// grouped without guessing which of them a worktree belongs to, and attaching
	// it to the wrong project is worse than leaving it at the top level.
	parentsByRepo := make(map[string]int)
	for _, it := range items {
		if m, ok := meta[it.Path]; ok && !m.IsWorktree && m.Repo != "" {
			parentsByRepo[m.Repo]++
		}
	}

	var nodes []projectRowNode
	nodeByRepo := make(map[string]int)
	// A synthesized parent is created on first sight of an orphaned worktree row,
	// so it takes that row's place in the order.
	for i, it := range items {
		m, known := meta[it.Path]
		groupable := known && m.Repo != "" && parentsByRepo[m.Repo] <= 1
		switch {
		case groupable && !m.IsWorktree:
			if idx, ok := nodeByRepo[m.Repo]; ok {
				// The repository's own row arrives after one of its worktrees —
				// it is the older of the two. It takes over the parent that was
				// synthesized for the worktree, so one repository never grows two
				// parent rows.
				nodes[idx].Row = it
				nodes[idx].rank = max(nodes[idx].rank, i)
				nodes[idx].weakRank = max(nodes[idx].weakRank, i)
				continue
			}
			nodeByRepo[m.Repo] = len(nodes)
			nodes = append(nodes, projectRowNode{Row: it, rank: i, weakRank: i})
		case groupable && m.IsWorktree:
			idx, ok := nodeByRepo[m.Repo]
			if !ok {
				idx = len(nodes)
				nodeByRepo[m.Repo] = idx
				nodes = append(nodes, projectRowNode{
					Row:      synthesizedProjectRow(m),
					rank:     -1,
					weakRank: i,
				})
			}
			if !rowHasLiveSession(it) {
				// Nested membership is live sessions only: a session-less worktree
				// is not something to attach to from here, since sessions are born
				// with the worktree window or with a drain.
				nodes[idx].weakRank = max(nodes[idx].weakRank, i)
				continue
			}
			nodes[idx].Children = append(nodes[idx].Children, it)
			nodes[idx].rank = max(nodes[idx].rank, i)
			nodes[idx].weakRank = max(nodes[idx].weakRank, i)
		default:
			nodes = append(nodes, projectRowNode{Row: it, rank: i, weakRank: i})
		}
	}

	for i := range nodes {
		if nodes[i].rank < 0 {
			nodes[i].rank = nodes[i].weakRank
		}
	}
	sort.SliceStable(nodes, func(a, b int) bool { return nodes[a].rank < nodes[b].rank })
	return nodes
}

// flattenProjectRows renders the tree as the rows the picker lists: every parent,
// plus the children of the expanded ones. A collapsed parent renders its
// disclosure triangle and nothing else — no count, no summary of what is folded
// away.
func flattenProjectRows(nodes []projectRowNode, expanded map[string]bool) []ui.Item {
	rows := make([]ui.Item, 0, len(nodes))
	for _, n := range nodes {
		row := fuseNestedGlyph(n.Row)
		if len(n.Children) > 0 {
			row.Disclosure = iconRowCollapsed
			if expanded[n.Row.Path] {
				row.Disclosure = iconRowExpanded
			}
		}
		rows = append(rows, row)
		if !expanded[n.Row.Path] {
			continue
		}
		for _, c := range n.Children {
			child := fuseNestedGlyph(c)
			child.Depth = 1
			child.Name = nestedChildLabel(c.Name)
			rows = append(rows, child)
		}
	}
	return rows
}

// fuseNestedGlyph collapses the icon and marker columns into the one glyph
// column nested mode renders. Every live session reads the same, whatever it
// hosts: which kind of Work that is belongs to the Work dashboard, and this list
// answers "what can I attach to". A Map session is the single exception, because
// you enter it to decide rather than to sit in a checkout. Unread output outranks
// both — it is the row you are being asked to look at.
func fuseNestedGlyph(item ui.Item) ui.Item {
	switch {
	case item.Icon == iconAttention:
		// Kept as it is — the kind of session matters less than being told to
		// look at this one.
	case item.Marker == iconMapSession:
		item.Icon = iconNestedMapSession
	case item.Icon != "":
		item.Icon = iconDirSession
	}
	item.Marker = ""
	return item
}

// nestedChildLabel drops the "<project>/" prefix from a child's rendered label:
// the level it sits on already says which project it belongs to. The row's Path
// and SessionName keep it, so the session this label stands for is still the one
// derived from the worktree directory.
func nestedChildLabel(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// synthesizedProjectRow is the parent nested mode invents for a repository with
// no row of its own, named after the repository's display name. A bare repo has
// no project row today — its worktrees are the rows — and a half-nested list is
// the worse artifact.
func synthesizedProjectRow(m projectRowMeta) ui.Item {
	name := m.RepoLabel
	if name == "" {
		name = m.Repo
	}
	return ui.Item{
		Name:    name,
		Path:    projectGroupPathPrefix + m.Repo,
		Context: m.Repo,
	}
}

// isSynthesizedProjectRow reports whether a row is a grouping header rather than
// a checkout. Every action that opens, kills or records a row skips it: there is
// nothing behind it to open.
func isSynthesizedProjectRow(item ui.Item) bool {
	return strings.HasPrefix(item.Path, projectGroupPathPrefix)
}

// rowHasLiveSession reads the icon column, which buildSessionAwareItemsWith
// fills in exactly when the row's session name is among the live ones — so the
// glyph a row already carries is the live-session fact, with nothing to recompute
// and no second source that could disagree with what is on screen.
func rowHasLiveSession(item ui.Item) bool {
	return item.Icon != ""
}
