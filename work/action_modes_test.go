package work

import "testing"

// An action that declares nothing is singular. The default is the whole of
// ADR-0246 decision 5's safety: a kind written before the field existed, or a
// verb added without a thought about batches, targets one container — bulk is
// only ever reached by someone writing the grant down.
func TestActionModesDefaultToSingular(t *testing.T) {
	silent := Action{Verb: VerbCopyName, Key: "y", Label: "copy name"}
	if silent.Modes.AllowsPlural() {
		t.Fatal("an action that declares no modes is plural")
	}
	if Singular.AllowsPlural() {
		t.Fatal("the Singular constant allows a plural run")
	}
	granted := Action{Verb: VerbCopyName, Key: "y", Label: "copy name", Modes: Plural}
	if !granted.Modes.AllowsPlural() {
		t.Fatal("an action granted Plural refuses a plural run")
	}
}
