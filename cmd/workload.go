package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/workload"
	"github.com/spf13/cobra"
)

var workloadCmd = &cobra.Command{
	Use:   "workload",
	Short: "Discover and manage local PRD workloads",
}

var workloadStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show discovered PRD workloads and their statuses",
	Args:  cobra.NoArgs,
	RunE:  runWorkloadStatus,
}

func init() {
	rootCmd.AddCommand(workloadCmd)
	workloadCmd.AddCommand(workloadStatusCmd)
}

func runWorkloadStatus(cmd *cobra.Command, args []string) error {
	return runWorkloadStatusWith(workload.DefaultDeps(), os.Stdout)
}

func runWorkloadStatusWith(d *workload.Deps, w io.Writer) error {
	cwd, err := d.FS.Getwd()
	if err != nil {
		return fmt.Errorf("workload status: %w", err)
	}

	result, err := workload.RefreshWith(d, cwd, workload.DefaultStatePathWith(d))
	if err != nil {
		return fmt.Errorf("workload status: %w", err)
	}

	workload.Render(w, result)
	return nil
}
