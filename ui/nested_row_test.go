package ui

import "testing"

// A child row shifts right as a whole, glyph column included: indenting only the
// name leaves the nesting nearly invisible. The disclosure glyph trails the name
// of the row that holds children, and a list that never nests renders exactly as
// it did before either field existed.
func TestPickerCellNestedRowIndentsWholeRowAndTrailsDisclosure(t *testing.T) {
	items := []Item{
		{Name: "hawk", Path: "/src/hawk", Icon: "■", Disclosure: "▾"},
		{Name: "fix-auth", Path: "/wt/hawk/fix-auth", Icon: "■", Depth: 1},
		{Name: "api", Path: "/src/api", Icon: "■"},
	}
	p := NewPicker(items)

	cases := []struct {
		item Item
		want string
	}{
		{items[0], " ■ hawk ▾"},
		{items[1], "   ■ fix-auth"},
		{items[2], " ■ api"},
	}
	for _, c := range cases {
		if got := p.pickerCell(c.item, RowState{}); got != c.want {
			t.Errorf("pickerCell(%q) = %q, want %q", c.item.Name, got, c.want)
		}
	}
}
