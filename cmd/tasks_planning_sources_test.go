package cmd

import (
	"strings"
	"testing"
)

func TestPlanningSourcesConventionUsesRepositoryAnswer(t *testing.T) {
	f := newConventionFixture(t)
	const body = "Read Planning sources with the repository's configured tracker tool."
	f.repoDoc(t, "planning-sources", body)
	prose, err := planningSourcesConvention(f.deps.tasksDeps())(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prose, body) || strings.Contains(prose, "No external source of truth is configured") {
		t.Fatalf("wrong planning-sources answer:\n%s", prose)
	}
}
