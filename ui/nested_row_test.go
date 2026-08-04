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

// A list whose rows carry no marker reserves no gutter for one — which is what
// makes the project dashboard's fused glyph column literally one column wide, in
// either display mode, with no picker change of its own.
func TestPickerCellReservesNoMarkerGutterWithoutMarkers(t *testing.T) {
	fused := []Item{{Name: "hawk", Path: "/src/hawk", Icon: "■"}, {Name: "cold", Path: "/src/cold"}}
	p := NewPicker(fused)
	if got, want := p.pickerCell(fused[0], RowState{}), " ■ hawk"; got != want {
		t.Errorf("live row = %q, want %q", got, want)
	}
	if got, want := p.pickerCell(fused[1], RowState{}), "   cold"; got != want {
		t.Errorf("session-less row = %q, want %q — one blank icon column, no marker gutter", got, want)
	}

	// One marker anywhere is what buys the second column, so the assertion above is
	// about the rows and not about the picker having lost the ability.
	twoColumns := append(append([]Item(nil), fused...), Item{Name: "set", Path: "/src/set", Icon: "■", Marker: "▲"})
	if got, want := NewPicker(twoColumns).pickerCell(fused[0], RowState{}), "   ■ hawk"; got != want {
		t.Errorf("live row beside a marked row = %q, want %q", got, want)
	}
}
