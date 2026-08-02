// Package ref names a piece of Work — a container, or one item inside it —
// independently of the kind that produced it. It is deliberately a leaf: it
// imports no pop package, so both `store` (which must name a Checkout claim's
// holder) and `work` (which is consumed by the kinds) can name the same
// identity without an import cycle. Defining it in `store` and aliasing it
// outward was rejected — the persistence layer owning the domain's identity
// type is backwards.
package ref

import (
	"fmt"
	"strings"
)

// Kind is the closed enum of Work kinds. Closed is the point: "keep future
// kinds expressible" is a constraint on the container+item model, not a plugin
// requirement, and every new kind is a deliberate edit here.
type Kind string

const (
	// KindTaskSet is a Task set: a container of tasks.
	KindTaskSet Kind = "task-set"
	// KindMap is a Map: a container of Decision tickets.
	KindMap Kind = "map"
	// KindRoutine is a Routine: a container of runs.
	KindRoutine Kind = "routine"
)

// Kinds returns every Work kind in fixed precedence order — Task sets, then
// Maps, then Routines. Callers that fan out over all kinds (the registry, the
// dashboard) render in this order.
func Kinds() []Kind { return []Kind{KindTaskSet, KindMap, KindRoutine} }

// Valid reports whether k is one of the enum's members. Anything crossing a
// boundary pop does not control — a database row, a manifest, a CLI argument —
// is a string until this says otherwise.
func (k Kind) Valid() bool {
	switch k {
	case KindTaskSet, KindMap, KindRoutine:
		return true
	}
	return false
}

func (k Kind) String() string { return string(k) }

// ParseKind converts s to a Kind, refusing anything outside the enum.
func ParseKind(s string) (Kind, error) {
	k := Kind(s)
	if !k.Valid() {
		return "", fmt.Errorf("unknown work kind %q", s)
	}
	return k, nil
}

// WorkRef identifies a Work container, or one item within it: a Task set and
// its task, a Map and its Decision ticket, a Routine and its run. An empty
// ItemID means the ref names the container itself.
type WorkRef struct {
	Kind        Kind
	ContainerID string
	ItemID      string
}

// Container returns the ref with its item segment dropped, so a holder of an
// item ref can name the container that owns it (the registry keys containers).
func (r WorkRef) Container() WorkRef {
	r.ItemID = ""
	return r
}

// IsItem reports whether the ref names an item rather than a whole container.
func (r WorkRef) IsItem() bool { return r.ItemID != "" }

// IsZero reports whether the ref names nothing — the absent-holder value.
func (r WorkRef) IsZero() bool { return r == WorkRef{} }

// String renders the ref as `task-set:2026-08-02-foo/03`, dropping the `/item`
// segment when the ref names a container. This is the one rendering of Work
// identity every surface uses, so a log line, a deferral message and a
// dashboard cell all name the same thing the same way.
func (r WorkRef) String() string {
	if r.IsZero() {
		return ""
	}
	s := string(r.Kind) + ":" + r.ContainerID
	if r.ItemID != "" {
		s += "/" + r.ItemID
	}
	return s
}

// Parse reads back what String renders, refusing an unknown kind, a missing
// container segment, or an empty item segment after the separator (`foo/` is a
// typo, not a container ref).
func Parse(s string) (WorkRef, error) {
	kindPart, rest, ok := strings.Cut(s, ":")
	if !ok || rest == "" {
		return WorkRef{}, fmt.Errorf("malformed work ref %q: want kind:container[/item]", s)
	}
	kind, err := ParseKind(kindPart)
	if err != nil {
		return WorkRef{}, err
	}
	container, item, hasItem := strings.Cut(rest, "/")
	if container == "" || (hasItem && item == "") {
		return WorkRef{}, fmt.Errorf("malformed work ref %q: want kind:container[/item]", s)
	}
	return WorkRef{Kind: kind, ContainerID: container, ItemID: item}, nil
}
