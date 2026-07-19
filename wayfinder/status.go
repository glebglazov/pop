package wayfinder

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

const destinationGistMaxLen = 48

// BuildStatus derives the status snapshot from on-disk maps.
func BuildStatus(d *Deps, cwd string, includeAll bool) (StatusSnapshot, error) {
	maps, err := ScanMaps(d, cwd)
	if err != nil {
		return StatusSnapshot{}, err
	}
	rows := make([]StatusRow, 0, len(maps))
	for _, m := range maps {
		row := mapToStatusRow(m)
		if includeAll || visibleByDefault(row) {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return StatusSnapshot{Rows: rows}, nil
}

func visibleByDefault(row StatusRow) bool {
	if row.Archived {
		return false
	}
	switch row.Status {
	case MapDone, MapAbandoned:
		return false
	default:
		return true
	}
}

func mapToStatusRow(m Map) StatusRow {
	row := StatusRow{
		ID:              m.ID,
		DestinationGist: DestinationGist(m.Destination, destinationGistMaxLen),
		Archived:        m.Archived,
	}
	if m.Malformed {
		row.Malformed = true
		row.Status = MapMalformed
		row.MalformedSummary = m.MalformedReason
		return row
	}
	row.Status = m.Status
	counts := CountTickets(m.Tickets)
	row.Counts = counts
	row.FrontierSize = len(Frontier(m.Tickets))
	return row
}

// RenderStatus prints the wayfinder status table as plain text.
func RenderStatus(out io.Writer, snap StatusSnapshot) error {
	if len(snap.Rows) == 0 {
		fmt.Fprintln(out, "No wayfinder maps.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MAP\tSTATUS\tDESTINATION\tOPEN\tCLAIMED\tRESOLVED\tFRONTIER")
	for _, row := range snap.Rows {
		status := string(row.Status)
		if row.Archived {
			status += " [archived]"
		}
		if row.Malformed {
			dest := row.MalformedSummary
			if dest == "" {
				dest = "malformed"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t-\t-\t-\t-\n", row.ID, status, dest)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\n",
			row.ID,
			status,
			row.DestinationGist,
			row.Counts.Open,
			row.Counts.Claimed,
			row.Counts.Resolved,
			row.FrontierSize,
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return nil
}

// Status renders wayfinder status using default dependencies.
func Status(out io.Writer, includeAll bool) error {
	return StatusWith(DefaultDeps(), out, includeAll)
}

// StatusWith renders wayfinder status with injected dependencies.
func StatusWith(d *Deps, out io.Writer, includeAll bool) error {
	snap, err := BuildStatus(d, "", includeAll)
	if err != nil {
		return err
	}
	return RenderStatus(out, snap)
}

// FormatStatusCell returns the display status for tests and future surfaces.
func FormatStatusCell(row StatusRow) string {
	status := string(row.Status)
	if row.Archived {
		status += " [archived]"
	}
	return strings.TrimSpace(status)
}
