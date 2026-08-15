package work

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Snapshot is the data model for `pop work dashboard`.
type Snapshot struct {
	// Containers are every loaded Work container in snapshot order — kind
	// precedence, then each kind's own comparator. There is no second list beside
	// it: a dashboard row is one of these.
	Containers []Container
	// Summary is every kind's header phrases in kind order, already pluralised.
	// SummaryLine joins them.
	Summary []string
	// ModelSkips are the Effort model skips in force at build time (ADR-0168),
	// ordered by preset then model. They are machine-global rather than
	// per-container, which is why they ride the snapshot and render as a footer
	// one-liner rather than as a cell. Empty is the steady state.
	ModelSkips []ModelSkip
	// Attribution is the containers the pane this build was launched from belongs
	// to, nil when it belongs to none of the kinds on this page. Only a build
	// handed pane facts can carry one: a rebuild is not a launch, so a poll's
	// snapshot can never re-seed a cursor the human has since moved (ADR-0201
	// decision 4).
	Attribution *Attribution
}

// ModelSkip is one Effort model skip still in force: the preset whose ladder
// entry pop is walking past, the `--model` token that entry pins, and when the
// skip lifts. A zero Until is a permanent skip (ADR-0168).
type ModelSkip struct {
	Preset string
	Model  string
	Until  time.Time
	// StatedUntil is the reset the provider's refusal claimed, which the capped
	// Until may deliberately fall short of (ADR-0168). Zero when it claimed none.
	StatedUntil time.Time
}

// BuildSnapshot builds one point-in-time Work snapshot from a wired list of
// kinds. It is the whole of the builder: every read of the filesystem and of
// pop.db happens inside a kind's Load, so `work` itself scans nothing and knows
// nothing about what a task set or a Map is.
//
// Ordering is fixed kind precedence — the closed enum's order — then the owning
// kind's own comparator. Nothing compares across kinds, which is the point: with
// no shared status taxonomy there is no cross-kind ranking to derive.
func BuildSnapshot(kinds []Kind) (Snapshot, error) {
	return BuildSnapshotForPane(kinds, PaneFacts{})
}

// BuildSnapshotForPane is BuildSnapshot plus the facts of the pane the surface
// was launched from: once every load is in hand, the kinds are asked which
// container that pane belongs to. Asking here rather than before the build is
// what keeps the answer kind-side — a caller resolving it up front would need a
// switch over kinds in exactly the layer the seam exists to keep free of them
// (ADR-0201 decision 3).
func BuildSnapshotForPane(kinds []Kind, facts PaneFacts) (Snapshot, error) {
	ordered := kindsInPrecedence(kinds)
	loaded := make([][]Container, len(ordered))
	for i, k := range ordered {
		containers, err := k.Load()
		if err != nil {
			return Snapshot{}, fmt.Errorf("work: load %s containers: %w", k.ID(), err)
		}
		// The builder stamps the kind rather than trusting each container to carry
		// it: the loader that produced it is the authority on what kind it is.
		for j := range containers {
			containers[j].Kind = k.ID()
		}
		sortWithin(k, containers)
		loaded[i] = containers
	}

	snap := Snapshot{}
	for i, k := range ordered {
		snap.Containers = append(snap.Containers, loaded[i]...)
		snap.Summary = append(snap.Summary, k.Summary(loaded[i])...)
		skips, err := kindModelSkips(k)
		if err != nil {
			return Snapshot{}, err
		}
		snap.ModelSkips = append(snap.ModelSkips, skips...)
	}
	snap.Attribution = AttributePane(ordered, facts)
	return snap, nil
}

// kindsInPrecedence returns the wired kinds in fixed precedence order, dropping
// nils so a partially wired caller degrades to the kinds it did supply.
func kindsInPrecedence(kinds []Kind) []Kind {
	ordered := make([]Kind, 0, len(kinds))
	for _, k := range kinds {
		if k != nil {
			ordered = append(ordered, k)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return kindRank(ordered[i].ID()) < kindRank(ordered[j].ID())
	})
	return ordered
}

// sortWithin applies one kind's comparator to its own containers. A kind that
// declines to order them (a nil-safe comparator returning false both ways) keeps
// its Load order.
func sortWithin(k Kind, containers []Container) {
	sort.SliceStable(containers, func(i, j int) bool {
		return k.Less(containers[i], containers[j])
	})
}

// kindModelSkips collects a kind's machine-global model skips when it carries
// any. The type assertion is the extension point: a kind with no global
// footnotes implements nothing and contributes nothing.
func kindModelSkips(k Kind) ([]ModelSkip, error) {
	src, ok := k.(SkipSource)
	if !ok {
		return nil, nil
	}
	skips, err := src.ModelSkips()
	if err != nil {
		return nil, fmt.Errorf("work: %s model skips: %w", k.ID(), err)
	}
	return skips, nil
}

// SummaryLine joins every kind's header phrases in kind order — the one header
// text a read surface prints.
func (s Snapshot) SummaryLine() string {
	return strings.Join(s.Summary, " · ")
}

// CountPhrase renders one header phrase — "1 task set", "3 maps" — so every
// kind's Summary pluralises the same way. It is the one piece of header text
// `work` owns; what a kind counts is the kind's own business.
func CountPhrase(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// ContainersOfKind returns the snapshot's containers of one kind, in snapshot
// order.
func (s Snapshot) ContainersOfKind(id KindID) []Container {
	var out []Container
	for _, c := range s.Containers {
		if c.Kind == id {
			out = append(out, c)
		}
	}
	return out
}
