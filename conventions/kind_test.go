package conventions

import "testing"

// TestEveryKindDeclaresAConsumptionShape is what makes the property a decision
// the author of the next kind owes rather than one each call site invents: a
// kind added to the enum without a shape fails here (ADR-0227 decision 1).
func TestEveryKindDeclaresAConsumptionShape(t *testing.T) {
	for _, kind := range Kinds() {
		switch kind.Shape() {
		case ShapeRoleDriving, ShapeStepInforming:
		default:
			t.Errorf("kind %s declares no consumption shape", kind)
		}
	}
}

// TestConsumptionShapesAsDecided pins the split ADR-0227 decision 1 made: the
// two kinds that are an agent's entire mandate are the prompt body, and the two
// that are a fact another prompt needs stay a block inside pop's own.
func TestConsumptionShapesAsDecided(t *testing.T) {
	for kind, want := range map[Kind]Shape{
		KindVerification: ShapeRoleDriving,
		KindRefine:       ShapeRoleDriving,
		KindCommits:      ShapeStepInforming,
		KindIssueTracker: ShapeStepInforming,
	} {
		if got := kind.Shape(); got != want {
			t.Errorf("kind %s declares shape %q, want %q", kind, got, want)
		}
	}
}
