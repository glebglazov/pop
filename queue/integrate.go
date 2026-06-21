package queue

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/integration"
	"github.com/glebglazov/pop/tasks"
)

// IntegrationResult describes the outcome of a human-triggered clean set
// integration.
type IntegrationResult = integration.IntegrationResult

// IntegrationOptions controls the attended conflict-resolution path.
type IntegrationOptions = integration.IntegrationOptions

// Integrate merges a DONE set into its working branch.
func Integrate(d *Deps, cfg *config.Config, setID string, out io.Writer) (IntegrationResult, error) {
	return IntegrateWithOptions(d, cfg, setID, out, IntegrationOptions{In: tasks.NonInteractiveReader{}})
}

// IntegrateWithOptions merges a completed set into its working branch.
func IntegrateWithOptions(d *Deps, cfg *config.Config, setID string, out io.Writer, opts IntegrationOptions) (IntegrationResult, error) {
	return integration.IntegrateWithOptions(queueIntegrationDeps(d), cfg, setID, out, opts, queueIntegrateHooks(d))
}

// LookupMergeability returns the mergeability record for setID.
func LookupMergeability(d *Deps, setID string) (MergeabilityRecord, bool, error) {
	rec, ok, err := integration.Lookup(queueIntegrationDeps(d), setID)
	return mergeabilityRecordFromIntegration(rec), ok, err
}

// RecordImplementMergeability computes and records mergeability after implement drain.
func RecordImplementMergeability(d *Deps, projectPath, runtimePath, setID, project string) error {
	return integration.RecordImplementMergeability(queueIntegrationDeps(d), projectPath, runtimePath, setID, project)
}

// BuildConflictResolutionPrompt builds the agent prompt for conflict resolution.
func BuildConflictResolutionPrompt(rec MergeabilityRecord, workingPath, branch string) string {
	return integration.BuildConflictResolutionPrompt(mergeabilityRecordToIntegration(rec), workingPath, branch)
}

func findIntegrationRecord(d *Deps, setID string) (string, MergeabilityRecord, bool, error) {
	if d == nil {
		d = DefaultDeps()
	}
	store, err := integration.Load(d.Tasks)
	if err != nil {
		return "", MergeabilityRecord{}, false, err
	}
	if store == nil || len(store.Records) == 0 {
		return "", MergeabilityRecord{}, false, nil
	}
	setID = strings.TrimSpace(setID)
	var keys []string
	for key, rec := range store.Records {
		if rec.SetID == setID {
			keys = append(keys, key)
		}
	}
	switch len(keys) {
	case 0:
		return "", MergeabilityRecord{}, false, nil
	case 1:
		return keys[0], mergeabilityRecordFromIntegration(store.Records[keys[0]]), true, nil
	default:
		sort.Strings(keys)
		var b strings.Builder
		fmt.Fprintf(&b, "queue: set %q is ambiguous; awaiting integration in:", setID)
		for _, key := range keys {
			rec := store.Records[key]
			fmt.Fprintf(&b, "\n  %s (%s)", rec.Project, rec.RuntimePath)
		}
		return "", MergeabilityRecord{}, false, fmt.Errorf("%s", b.String())
	}
}

func integrateCleanSet(d *Deps, cfg *config.Config, key string, rec MergeabilityRecord, out io.Writer, source string) (IntegrationResult, error) {
	return integration.IntegrateKnownRecord(queueIntegrationDeps(d), cfg, key, mergeabilityRecordToIntegration(rec), out, source, queueIntegrateHooks(d))
}

func resolveIntegrationScan(d *Deps, cfg *config.Config, rec MergeabilityRecord) (projectScan, error) {
	path, err := integration.ResolveIntegrationScan(queueIntegrationDeps(d), cfg, mergeabilityRecordToIntegration(rec))
	if err != nil {
		return projectScan{}, err
	}
	return projectScan{RuntimePath: path, ProjectPath: path}, nil
}
