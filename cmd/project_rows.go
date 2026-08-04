package cmd

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work/ref"
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

// attributeMapSessions gives every live Map session a project and a name. A Map
// session is a standalone row today — it is not a checkout, so nothing in the
// project walk knows about it — and it arrives as the one long unattributed row
// in an otherwise grouped list (ADR-0185).
//
// A Map is rooted at the Trunk worktree of exactly one repository, so tmux's own
// start directory for the session says which project it belongs to. The fact
// rides on the list-sessions call the stamp already costs, so attribution adds no
// process spawn and no filesystem I/O here. The directory is matched to a project
// *group*, never to the individual row it names: a bare repo's Trunk is itself one
// of the worktree rows, and matching that row would invent a third level for one
// row type.
//
// The returned rows and metadata are the caller's inputs plus these facts. Rows
// keep their Path, so the session a row attaches to and the recency it sorts by
// are both untouched; only the label and the grouping change. A directory
// resolving to no configured project is the honest fallback, not an error: the
// start directory is mutable, so an attribution can go stale, and the map that
// happens to degrades to the top-level row it has today.
func attributeMapSessions(items []ui.Item, workSessions map[string]tmuxmod.WorkSession, projectMeta map[string]projectRowMeta) ([]ui.Item, map[string]projectRowMeta) {
	rows := make([]ui.Item, len(items))
	copy(rows, items)
	meta := projectMeta
	ownMeta := false
	var byDir map[string]projectRowMeta
	for i, it := range rows {
		id, ok := mapSessionID(it, workSessions)
		if !ok {
			continue
		}
		if byDir == nil {
			byDir = projectMetaByDir(projectMeta)
		}
		group, attributed := byDir[filepath.Clean(workSessions[standaloneSessionName(it)].Dir)]
		if !attributed {
			rows[i].Name = id
			continue
		}
		// The prefixed name is what flat mode and a typed query render, and it is
		// what nestedChildLabel strips back down to the id one level in — the same
		// rule a worktree row follows.
		rows[i].Name = projectGroupLabel(group) + "/" + id
		if !ownMeta {
			// Copied before the first write: the project metadata is computed once per
			// invocation, while Work sessions are re-read every picker iteration, so
			// this pass must not leave last iteration's attributions behind.
			meta = cloneProjectRowMeta(projectMeta)
			ownMeta = true
		}
		meta[it.Path] = projectRowMeta{IsWorktree: true, Repo: group.Repo, RepoLabel: group.RepoLabel}
	}
	return rows, meta
}

// mapSessionID reports the map id a row's session hosts, reading the Work stamp
// rather than the session name: the name is pop's convention, the stamp is the
// session's own answer. The name is the fallback for a session stamped with a kind
// but no id, which is a session pop could badge but not resolve.
func mapSessionID(item ui.Item, workSessions map[string]tmuxmod.WorkSession) (string, bool) {
	if !isStandaloneSession(item) {
		return "", false
	}
	session := standaloneSessionName(item)
	ws, ok := workSessions[session]
	if !ok || ref.Kind(ws.Kind) != ref.KindMap {
		return "", false
	}
	id := ws.ID
	if id == "" {
		id = wayfinder.MapIDFromSession(session)
	}
	return id, id != ""
}

// projectMetaByDir keys the row metadata by cleaned directory, which is what a
// tmux start directory is matched against. Paths are compared as written — no
// symlink resolution, because that is the filesystem I/O this derivation exists to
// avoid, and a start directory pop itself created is the path pop was configured
// with.
func projectMetaByDir(projectMeta map[string]projectRowMeta) map[string]projectRowMeta {
	byDir := make(map[string]projectRowMeta, len(projectMeta))
	for path, m := range projectMeta {
		byDir[filepath.Clean(path)] = m
	}
	return byDir
}

func projectGroupLabel(m projectRowMeta) string {
	if m.RepoLabel != "" {
		return m.RepoLabel
	}
	return m.Repo
}

func cloneProjectRowMeta(meta map[string]projectRowMeta) map[string]projectRowMeta {
	out := make(map[string]projectRowMeta, len(meta)+1)
	for k, v := range meta {
		out[k] = v
	}
	return out
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

// buildProjectRows arranges the session-aware rows for display. Flat mode lists
// the same rows it always did — every worktree, session or not, under its full
// "<project>/<worktree>" name, which is what makes a flat list legible — and only
// fuses their glyphs. Nested mode returns a different row set, not a rearrangement
// of the same one: a project's live worktree sessions become its children and its
// session-less worktrees drop out, reachable by typing a query instead.
func buildProjectRows(items []ui.Item, meta map[string]projectRowMeta, display config.WorktreeDisplay, expanded map[string]bool) []ui.Item {
	if display != config.WorktreeDisplayNested {
		return fuseGlyphColumns(items, display)
	}
	return flattenProjectRows(nestProjectRows(items, meta), expanded)
}

// fuseGlyphColumns fuses a whole row slice, into a new slice: the rows it is given
// are the caller's, reused by projectRowTree and by the next iteration of the
// picker loop, so display work must never write into them.
func fuseGlyphColumns(items []ui.Item, display config.WorktreeDisplay) []ui.Item {
	rows := make([]ui.Item, len(items))
	for i, it := range items {
		rows[i] = fuseGlyphColumn(it, display)
	}
	return rows
}

// projectRowTree wires nested mode into the picker's arrow gestures: which rows a
// browse lists, which rows a query searches, and where expansion is remembered.
// The rows it hands back come from the session state the picker was opened with —
// nothing here rediscovers projects, reads config or forks git, so a gesture costs
// one re-arrangement of a slice.
//
// expanded is the caller's own map, so the tree writes into state that outlives
// the picker: the project loop closes and reopens the picker for some actions, and
// the operator must not lose the tree they opened. It is process memory only,
// deliberately persisted nowhere — a fresh dashboard opens collapsed.
func projectRowTree(sessionRows []ui.Item, meta map[string]projectRowMeta, expanded map[string]bool) ui.Tree {
	return ui.Tree{
		Rows: func(query string) []ui.Item {
			if query != "" {
				return queryProjectRows(sessionRows)
			}
			return buildProjectRows(sessionRows, meta, config.WorktreeDisplayNested, expanded)
		},
		SetExpanded: func(path string, expand bool) {
			if expand {
				expanded[path] = true
				return
			}
			delete(expanded, path)
		},
	}
}

// queryProjectRows is what a typed query searches in nested mode: the whole
// universe at depth zero under the full "<project>/<worktree>" names, with no
// grouping and nothing auto-expanded. Nested membership holds live sessions only,
// so this flat pass is what keeps a cold worktree reachable at all — and, because
// the names carry their prefix here, a query can match on it.
func queryProjectRows(items []ui.Item) []ui.Item {
	// The glyph column is the one nested mode renders throughout: typing is not a
	// different list with a different vocabulary, it is the same list unfolded.
	return fuseGlyphColumns(items, config.WorktreeDisplayNested)
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
		row := fuseGlyphColumn(n.Row, config.WorktreeDisplayNested)
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
			child := fuseGlyphColumn(c, config.WorktreeDisplayNested)
			child.Depth = 1
			child.Name = nestedChildLabel(c.Name)
			rows = append(rows, child)
		}
	}
	return rows
}

// fuseGlyphColumn collapses the icon and marker columns into the one glyph column
// the project dashboard renders. The precedence is shared by both display modes —
// unread output, then Work kind, then session presence — and the kind glyph
// *replaces* the live-session glyph rather than sitting beside it, since a row
// cannot host Work without hosting a session.
//
// What the middle rank says is where the modes part, deliberately. Flat is the
// whole inventory and keeps every distinction it can draw: a Map, a Task set, a
// Routine, a bare standalone session and a live checkout each read as themselves.
// Nested answers the narrower "what can I attach to", so every live session reads
// alike and only a Map stands out — you enter one to decide rather than to sit in a
// checkout. Both spell a Map hollow, keeping the filled diamond off this surface.
func fuseGlyphColumn(item ui.Item, display config.WorktreeDisplay) ui.Item {
	switch {
	case item.Icon == "":
		// No session, no glyph. This arm also refuses to promote a stray kind badge
		// into the one column: without a live session there is nothing for a badge
		// to be about.
	case item.Icon == iconAttention:
		// Kept as it is — the kind of session matters less than being told to
		// look at this one.
	case display == config.WorktreeDisplayNested:
		if item.Marker == iconMapSession {
			item.Icon = iconHollowMapSession
		} else {
			item.Icon = iconDirSession
		}
	case item.Marker == iconMapSession:
		item.Icon = iconHollowMapSession
	case item.Marker != "":
		item.Icon = item.Marker
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
