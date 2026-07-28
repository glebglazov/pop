package integrate

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/config"
)

func RunUpdateExistingWith(
	rev string,
	cd *config.Deps,
	newDry, newReal func() *Deps,
	stdout, stderr io.Writer,
	verbose bool,
) error {
	result := updateStaleIntegrations(cd, newDry, newReal)

	PrintOutcomes(stdout, result.Outcomes, verbose, false)

	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "⚠ %s\n", w)
	}

	stampRevisionIfSuccess(rev, stateDepsFromConfig(cd, newReal()), result)
	return nil
}
