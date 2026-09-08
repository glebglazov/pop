## What to build

VCDR stops warning the user about Secure Input, because it no longer affects
anything VCDR does.

The warning was shipped to explain hotkeys dying under a held Secure Input flag.
That cause is gone: Carbon hotkeys fire while the flag is held, and posting a
paste keystroke was measured as unaffected too, so a capture records, transcribes
and delivers normally throughout. The current wording — that macOS delivers no key
events to VCDR and no hotkey can fire — would be actively false.

A standing warning about a condition that degrades nothing teaches the user to
ignore the surface it sits on, and that surface also carries the real microphone
and Accessibility warnings. So it goes rather than being reworded. The reasoning is
already recorded in the architecture decision, so nothing is lost by removing the
code.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `vcdr-mac/Hotkeys/SecureInputMonitor.swift`, its wiring in `AppDependencies`,
  and its surfaces in `MenuBarView`, `HomeTabView` and `MainWindowView`.
- `vcdr-macTests/Hotkeys/SecureInputMonitorTests.swift` goes with it.
- The Home tab's "Hotkeys Blocked" section is the visible one; the menu bar
  states it under the hotkey it contradicts.
- Do not touch the Permissions Required section in the same view — that one stays.
- Build: `xcodebuild -scheme vcdr-mac -destination 'platform=macOS' build`
- Test: `xcodebuild test -scheme vcdr-mac -destination 'platform=macOS' -quiet`

## Type

AFK

## Acceptance criteria

- [x] No surface mentions Secure Input, and the monitor and its test are deleted
- [x] The microphone and Accessibility warnings on the Home tab are unchanged
- [x] Nothing polls the Secure Input flag any more
- [x] Build succeeds and the suite is green

## Blocked by

- 06-delete-the-event-tap-mechanism
