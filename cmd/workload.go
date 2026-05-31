package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/workload"
	"github.com/spf13/cobra"
)

var (
	workloadProject string
	workloadPath    string
	workloadDefPath string
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

var workloadSetPriorityCmd = &cobra.Command{
	Use:   "set-priority PRD PRIORITY",
	Short: "Set a registered PRD priority",
	Args:  cobra.ExactArgs(2),
	RunE:  runWorkloadSetPriority,
}

func init() {
	rootCmd.AddCommand(workloadCmd)
	workloadCmd.AddCommand(workloadStatusCmd)
	workloadCmd.AddCommand(workloadSetPriorityCmd)

	workloadCmd.PersistentFlags().StringVar(&workloadProject, "project", "", "Select project by exact picker-visible name")
	workloadCmd.PersistentFlags().StringVar(&workloadPath, "path", "", "Select project by path (normalized to git checkout root)")
	workloadCmd.PersistentFlags().StringVar(&workloadDefPath, "workload-definition-path", "", "Exact workload definition directory (not normalized to git root)")
}

func workloadResolveInput() workload.ResolveInput {
	return workload.ResolveInput{
		ProjectName:        workloadProject,
		Path:               workloadPath,
		DefinitionOverride: workloadDefPath,
	}
}

func runWorkloadStatus(cmd *cobra.Command, args []string) error {
	return runWorkloadStatusWith(workload.DefaultDeps(), os.Stdout)
}

var workloadConfigLoad = func(path string) (*config.Config, error) {
	return config.Load(path)
}

func runWorkloadStatusWith(d *workload.Deps, w io.Writer) error {
	resolved, err := workload.ResolvePathsWith(d, workloadProjectDeps(), workloadConfigLoad, workloadResolveInput())
	if err != nil {
		return fmt.Errorf("workload status: %w", err)
	}

	result, err := workload.RefreshWith(d, resolved.DefinitionPath, workload.DefaultStatePathWith(d))
	if err != nil {
		return fmt.Errorf("workload status: %w", err)
	}

	workload.Render(w, result)
	return nil
}

func runWorkloadSetPriority(cmd *cobra.Command, args []string) error {
	return runWorkloadSetPriorityWith(workload.DefaultDeps(), os.Stdout, args[0], args[1])
}

func runWorkloadSetPriorityWith(d *workload.Deps, w io.Writer, prdID, priorityArg string) error {
	priority, err := strconv.Atoi(priorityArg)
	if err != nil {
		return fmt.Errorf("workload set-priority: invalid priority %q: %w", priorityArg, err)
	}

	result, err := workload.SetPriorityWith(d, workloadProjectDeps(), workloadConfigLoad, workloadResolveInput(), prdID, priority)
	if err != nil {
		return fmt.Errorf("workload set-priority: %w", err)
	}

	fmt.Fprintf(w, "Updated priority for %s: %d -> %d\n\n", result.PRDID, result.OldPriority, result.NewPriority)
	workload.Render(w, result.Refresh)
	return nil
}

func workloadProjectDeps() *project.Deps {
	return project.DefaultDeps()
}
