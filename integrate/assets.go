package integrate

import "embed"

//go:embed all:skills/pop
var skillFiles embed.FS

//go:embed issue-tracker.md
var issueTrackerDoc []byte

//go:embed extensions/pi/pop-status-sync.ts
var piExtensionFile []byte

//go:embed extensions/opencode/pop-status-sync.ts
var opencodeExtensionFile []byte
