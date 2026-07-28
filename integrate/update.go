package integrate

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/config"
)

func RunUpdateExistingWith(
	rev string,
	cd *config.Deps,
	newDeps func() *Deps,
	stdout, stderr io.Writer,
	verbose bool,
) error {
	result := updateStaleIntegrations(cd, newDeps)

	PrintOutcomes(stdout, result.Outcomes, verbose, false)

	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "⚠ %s\n", w)
	}

	stampRevisionIfSuccess(rev, stateDepsFromConfig(cd, newDeps()), result)
	return nil
}
