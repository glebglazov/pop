package ref

import (
	"os/exec"
	"strings"
	"testing"
)

func TestWorkRefRendersKindContainerItem(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ref  WorkRef
		want string
	}{
		{"container and item", WorkRef{Kind: KindTaskSet, ContainerID: "2026-08-02-foo", ItemID: "03"}, "task-set:2026-08-02-foo/03"},
		{"item segment omitted when empty", WorkRef{Kind: KindTaskSet, ContainerID: "2026-08-02-foo"}, "task-set:2026-08-02-foo"},
		{"map ticket", WorkRef{Kind: KindMap, ContainerID: "generalize-work", ItemID: "05"}, "map:generalize-work/05"},
		{"routine container", WorkRef{Kind: KindRoutine, ContainerID: "nightly-audit"}, "routine:nightly-audit"},
		{"zero ref names nothing", WorkRef{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ref.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
			if tc.want == "" {
				return
			}
			back, err := Parse(tc.want)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.want, err)
			}
			if back != tc.ref {
				t.Fatalf("Parse(%q) = %+v, want %+v", tc.want, back, tc.ref)
			}
		})
	}
}

func TestContainerDropsTheItemSegment(t *testing.T) {
	t.Parallel()
	item := WorkRef{Kind: KindMap, ContainerID: "generalize-work", ItemID: "05"}
	if !item.IsItem() {
		t.Fatal("IsItem() = false for a ref with an item id")
	}
	container := item.Container()
	if container.IsItem() || container.String() != "map:generalize-work" {
		t.Fatalf("Container() = %q, want map:generalize-work with no item", container)
	}
	if item.ItemID != "05" {
		t.Fatalf("Container() mutated its receiver: %+v", item)
	}
}

func TestKindEnumIsClosed(t *testing.T) {
	t.Parallel()
	for _, k := range Kinds() {
		if !k.Valid() {
			t.Fatalf("Kinds() member %q is not Valid", k)
		}
		parsed, err := ParseKind(string(k))
		if err != nil || parsed != k {
			t.Fatalf("ParseKind(%q) = %q, %v", k, parsed, err)
		}
	}
	if got := Kinds(); len(got) != 3 || got[0] != KindTaskSet || got[1] != KindMap || got[2] != KindRoutine {
		t.Fatalf("Kinds() = %v, want task-set, map, routine in precedence order", got)
	}
	for _, bad := range []string{"", "Task-Set", "goal", "task_set"} {
		if _, err := ParseKind(bad); err == nil {
			t.Fatalf("ParseKind(%q) accepted a non-member", bad)
		}
	}
}

func TestParseRefusesMalformedRefs(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "2026-08-02-foo", "task-set:", "task-set:foo/", "goal:foo", ":foo"} {
		if got, err := Parse(bad); err == nil {
			t.Fatalf("Parse(%q) = %+v, want an error", bad, got)
		}
	}
}

// TestRefIsALeafPackage is the property the package exists for: `store` must be
// able to name a Work ref, and `store` sits under everything. If ref ever grew
// an edge into another pop package, the first import from store would risk a
// cycle — so the guard is on the dependency, not on the eventual cycle.
func TestRefIsALeafPackage(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/glebglazov/pop/work/ref").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/glebglazov/pop") && dep != "github.com/glebglazov/pop/work/ref" {
			t.Fatalf("work/ref imports pop package %q — it must stay a leaf so store can import it", dep)
		}
	}
}
