package work

import "time"

// Artifact is one document a kind publishes about a container: a thing a human
// reads rather than acts on. It is not a Work item — an item is one advanceable
// thing inside a container and carries the status token its kind's ItemActions
// dispatch on, while an artifact has no status at all (ADR-0217).
//
// Like Container and Item it is plain data, and like Item.Type its type is the
// kind's own vocabulary: `work` never interprets it.
type Artifact struct {
	// Type is the kind's own classification of the document (a Task set's
	// `refine`, `spec`, `progress`).
	Type string
	// Name is what a reader sees on the row: the document's bare file name rather
	// than a humanised label, because a family whose members differ only by instant
	// reads better as the names that already carry those instants. A kind may use
	// the Path and container root to make copy-name more specific, as Task sets do
	// for refine reports under their refine/ directory.
	Name string
	// Path is the absolute path to the document. Absolute for the reason Item.File
	// is: the surfaces that read it — the Document peek, a CLI `--show` — hold no
	// directory of the kind's to resolve it against.
	Path string
	// At is when the document was written, the key its list is ordered by. A kind
	// answers it from whatever truthfully dates each member, so one list may mix
	// an instant read out of a file name with a modification time.
	At time.Time
}

// ArtifactSource is the optional extension a kind implements when it publishes
// documents about its containers: it offers them, says which verbs apply to one,
// and performs those verbs. A kind that publishes none simply does not implement
// it — consumers type-assert, the way they assert for SkipSource and for an
// advanceable kind — so no kind ever carries a stub returning nothing.
//
// PerformArtifact is a third method rather than a reuse of Kind.Perform because
// Perform takes an *Item, and an artifact is not an item; a surface holding an
// artifact row has nothing to pass there.
type ArtifactSource interface {
	// Artifacts returns the documents this container publishes, in the order a
	// reader should see them — the kind owns the order, because only it knows what
	// dates each member.
	Artifacts(Container) ([]Artifact, error)
	// ArtifactActions returns the verbs that apply to one artifact right now.
	ArtifactActions(Container, Artifact) []Action
	// PerformArtifact runs a verb over one artifact.
	PerformArtifact(Container, Artifact, Verb) (Outcome, error)
}
