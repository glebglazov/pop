package layout

import "sort"

// MinPaneCells is tmux's minimum pane extent along either axis. Layout
// realization refuses a container whose cell budget cannot give every child at
// least this many cells.
const MinPaneCells = 1

// CellBudget is the apportionable cell count for n children in a container of
// extent along the split axis. Tmux charges one cell per split to the surviving
// pane, so n children consume n-1 border cells.
func CellBudget(extent, n int) int {
	return extent - (n - 1)
}

// FitsMinCells reports whether budget can give each of n children at least min
// cells. Callers use this to detect unfittable layouts before touching tmux.
func FitsMinCells(budget, n, min int) bool {
	return budget >= n*min
}

// Apportion divides container extent into one cell count per child weight using
// largest-remainder apportionment against the cell budget. Weight 0 means 1.
// Returned counts always sum to CellBudget(extent, len(weights)).
func Apportion(extent int, weights []int) []int {
	n := len(weights)
	if n == 0 {
		return nil
	}
	budget := CellBudget(extent, n)
	if n == 1 {
		return []int{budget}
	}

	effective := make([]int, n)
	total := 0
	for i, w := range weights {
		ew := w
		if ew == 0 {
			ew = 1
		}
		effective[i] = ew
		total += ew
	}

	counts := make([]int, n)
	remainders := make([]int, n)
	assigned := 0
	for i, w := range effective {
		product := budget * w
		counts[i] = product / total
		remainders[i] = product % total
		assigned += counts[i]
	}

	extra := budget - assigned
	if extra == 0 {
		return counts
	}

	type idxRem struct {
		i   int
		rem int
	}
	order := make([]idxRem, n)
	for i := range weights {
		order[i] = idxRem{i: i, rem: remainders[i]}
	}
	sort.Slice(order, func(a, b int) bool {
		if order[a].rem != order[b].rem {
			return order[a].rem > order[b].rem
		}
		return order[a].i < order[b].i
	})
	for k := 0; k < extra; k++ {
		counts[order[k].i]++
	}
	return counts
}
