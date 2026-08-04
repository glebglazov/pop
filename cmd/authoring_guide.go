package cmd

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

// The authoring guides are one verb per Work kind rather than a
// `pop work authoring-guide <kind>` umbrella: the two texts are generated from
// different constants and share no body, so an umbrella would be a menu in front
// of two unrelated documents, discovered further from the family a session is
// already using (ADR-0183).
//
// Both are pure reads — they touch no store, need no repository and write
// nothing — so they answer identically in a virgin checkout and in a Map's own
// session.
var mapAuthoringGuideCmd = &cobra.Command{
	Use:   "authoring-guide",
	Short: "Print how to hand-write a Map: layout, templates and manifest fields",
	Long: `Print how to hand-write a Map by hand: the storage layout, the map.md and
ticket-file templates including the pop:generated markers, and the index.json
field list with its allowed values.

The enums, filenames, patterns and marker strings are generated from the same
constants "pop map register" validates against, so the printed rules cannot
drift from the enforced ones.

Read-only: it writes nothing and needs no map.`,
	Args: cobra.NoArgs,
	RunE: runMapAuthoringGuide,
}

var taskAuthoringGuideCmd = &cobra.Command{
	Use:   "authoring-guide",
	Short: "Print how to hand-write a task set: layout, templates, manifest fields and the typing rules",
	Long: `Print how to hand-write a Task set: the storage layout, the spec.md and
task-markdown templates, the index.json field list with its allowed values, and
the judgment rules — HITL/AFK typing, the effort heuristic, the vertical-slice
framing and the Orientation rule.

The enums, filenames and marker strings are generated from the same constants
"pop tasks register" validates against, so the printed rules cannot drift from
the enforced ones. This guide is authoritative: where an installed document
disagrees with it about the shape of these files, the guide wins.

Read-only: it writes nothing, registers nothing and needs no task set.`,
	Args: cobra.NoArgs,
	RunE: runTaskAuthoringGuide,
}

func init() {
	mapCmd.AddCommand(mapAuthoringGuideCmd)
	taskCmd.AddCommand(taskAuthoringGuideCmd)
}

func runMapAuthoringGuide(cmd *cobra.Command, _ []string) error {
	return writeAuthoringGuide(cmd.OutOrStdout(), wayfinder.AuthoringGuide())
}

func runTaskAuthoringGuide(cmd *cobra.Command, _ []string) error {
	return writeAuthoringGuide(cmd.OutOrStdout(), tasks.AuthoringGuide())
}

func writeAuthoringGuide(w io.Writer, guide string) error {
	_, err := fmt.Fprint(w, guide)
	return err
}
