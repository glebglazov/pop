package cmd

import "github.com/glebglazov/pop/release"

// pickerUpdateNotice returns the dimmed top-right Update notice text for the
// pickers, or "" when nothing should be shown. It renders only from the
// persisted cache and never blocks on the network (CONTEXT.md "Update check"):
// a stale cache schedules a background refresh for a later open, Dev builds and
// up-to-date binaries show nothing, and the notice surfaces at most once per
// calendar day across all pickers.
func pickerUpdateNotice() string {
	latest, show := release.PickerNotice(release.DefaultNoticeDeps(), buildVersion())
	if !show {
		return ""
	}
	return "update available: " + latest
}
