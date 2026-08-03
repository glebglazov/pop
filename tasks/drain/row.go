package drain

import "github.com/glebglazov/pop/work"

// A dashboard row is a Work container — there is no row model beside it — and
// the seam's data types live in the top-level work package (ADR-0143). These
// aliases preserve the drain surface's local vocabulary; the read surfaces
// re-export the same aliases under the same names.
type (
	DashboardRow      = work.Container
	DashboardSnapshot = work.Snapshot
)
