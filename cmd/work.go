package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Cross-concept work surface for planning, maps, and task sets",
	Long: `Cross-concept work surface for planning, maps, and task sets.

The Work dashboard lands in a later slice. show-path resolves this
repository's Task-storage root — the directory holding repo.json, tasks/,
and wayfinder/ — for humans and planning skills alike.`,
}

var workShowPathCmd = &cobra.Command{
	Use:   "show-path",
	Short: "Print this repository's Task-storage root, creating it on demand",
	Args:  cobra.NoArgs,
	Run:   runWorkShowPath,
}

func init() {
	rootCmd.AddCommand(workCmd)
	workCmd.AddCommand(workShowPathCmd)
}

func runWorkShowPath(cmd *cobra.Command, args []string) {
	err := runWorkShowPathWith(tasks.DefaultDeps(), os.Stdout)
	handleTaskExit(err)
}

func runWorkShowPathWith(d *tasks.Deps, w io.Writer) error {
	result, err := tasks.ShowStorageRoot(d, "")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, result.Path)
	return nil
}
