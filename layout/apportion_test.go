package layout

import (
	"slices"
	"testing"
)

func sumInts(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

func TestApportionEqualWeightsDivideEvenly(t *testing.T) {
	t.Parallel()
	extent := 27
	weights := []int{1, 1, 1, 1}
	got := Apportion(extent, weights)
	want := []int{6, 6, 6, 6}
	if !slices.Equal(got, want) {
		t.Fatalf("Apportion(%d, %v) = %v, want %v", extent, weights, got, want)
	}
	if sum := sumInts(got); sum != CellBudget(extent, len(weights)) {
		t.Fatalf("sum = %d, want budget %d", sum, CellBudget(extent, len(weights)))
	}
}

func TestApportionEqualWeightsWithRemainder(t *testing.T) {
	t.Parallel()
	extent := 25
	weights := []int{1, 1, 1}
	got := Apportion(extent, weights)
	want := []int{8, 8, 7}
	if !slices.Equal(got, want) {
		t.Fatalf("Apportion(%d, %v) = %v, want %v", extent, weights, got, want)
	}
}

func TestApportionLopsidedWeightsAvoidsZeroingSmallChild(t *testing.T) {
	t.Parallel()
	extent := 52
	weights := []int{1, 99}
	got := Apportion(extent, weights)
	// Naive integer division on the 51-cell budget would give 0 and 49.
	want := []int{1, 50}
	if !slices.Equal(got, want) {
		t.Fatalf("Apportion(%d, %v) = %v, want %v", extent, weights, got, want)
	}
}

func TestApportionSingleChild(t *testing.T) {
	t.Parallel()
	extent := 80
	got := Apportion(extent, []int{3})
	want := []int{80}
	if !slices.Equal(got, want) {
		t.Fatalf("Apportion(%d, [3]) = %v, want %v", extent, got, want)
	}
}

func TestApportionWeightZeroTreatedAsOne(t *testing.T) {
	t.Parallel()
	extent := 11
	got := Apportion(extent, []int{0, 0})
	want := []int{5, 5}
	if !slices.Equal(got, want) {
		t.Fatalf("Apportion(%d, [0,0]) = %v, want %v", extent, got, want)
	}
}

func TestApportionBudgetTooSmallForOneCellEach(t *testing.T) {
	t.Parallel()
	extent := 5
	weights := []int{1, 1, 1, 1}
	budget := CellBudget(extent, len(weights))
	if FitsMinCells(budget, len(weights), 1) {
		t.Fatalf("budget %d should not fit %d children at 1 cell each", budget, len(weights))
	}
	got := Apportion(extent, weights)
	if sum := sumInts(got); sum != budget {
		t.Fatalf("sum = %d, want budget %d", sum, budget)
	}
	min := got[0]
	for _, c := range got[1:] {
		if c < min {
			min = c
		}
	}
	if min >= 1 {
		t.Fatalf("counts = %v: expected some child below 1 when budget cannot fit all", got)
	}
}

func TestApportionCountsSumToBudget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		extent  int
		weights []int
	}{
		{120, []int{1, 2, 3}},
		{24, []int{200, 1}},
		{10, []int{1, 1, 1, 1, 1, 1}},
	}
	for _, tc := range cases {
		got := Apportion(tc.extent, tc.weights)
		if len(got) != len(tc.weights) {
			t.Fatalf("extent=%d weights=%v: len = %d, want %d", tc.extent, tc.weights, len(got), len(tc.weights))
		}
		wantSum := CellBudget(tc.extent, len(tc.weights))
		if sum := sumInts(got); sum != wantSum {
			t.Fatalf("extent=%d weights=%v: sum = %d, want budget %d", tc.extent, tc.weights, sum, wantSum)
		}
	}
}
