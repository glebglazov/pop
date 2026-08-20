package integrate

import (
	"embed"

	"github.com/glebglazov/pop/conventions/shipped"
)

//go:embed all:skills/pop
var skillFiles embed.FS

// issueTrackerDoc is pop's tracker document, owned by the conventions package
// as the `issue-tracker` shipped answer. Integration publishes the same bytes
// as a Shipped asset, so the document a skill reads off disk is the document
// the convention resolves to (ADR-0226).
var issueTrackerDoc = shipped.IssueTrackerDoc()

//go:embed extensions/pi/pop-status-sync.ts
var piExtensionFile []byte

//go:embed extensions/opencode/pop-status-sync.ts
var opencodeExtensionFile []byte
