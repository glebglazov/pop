## What to build

VCDR has one hotkey mechanism instead of two, and hotkeys work before the user
has granted Accessibility.

Every hotkey owner is on Carbon by now, so the event tap machinery is dead code.
Deleting it also removes the Accessibility guard that gates hotkey registration —
`RegisterEventHotKey` needs no grant, so hotkeys should be live on first launch.

That guard hides something, though: the call that prompts the user for
Accessibility is inside it, and it is the app's only prompt anywhere. Accessibility
is still genuinely required, for pasting and for reading the selection from other
apps. So the prompt moves to the first paste that finds itself untrusted, which
ties the ask to the feature that needs it. The Home tab already carries the
standing notice and already describes Accessibility as required for pasting, so
that surface needs nothing.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `vcdr-mac/Hotkeys/EventTapManager.swift` and
  `vcdr-mac/Hotkeys/GlobalHotkeyManager.swift` should both be gone at the end.
  Confirm nothing references them first — the display-string statics were moved
  in slice 01.
- The Accessibility guards sit at the top of `GlobalHotkeyManager.register` and
  in `DestinationHotkeyManager.register`. `requestAccessibilityPermissions()`,
  which calls `AXIsProcessTrustedWithOptions`, is the only prompt in the app.
- `PasteAction` already guards on `AXIsProcessTrusted()` and is the natural home
  for the prompt.
- `AppDependencies` reads `AXIsProcessTrusted()` for its polled permission state;
  `HomeTabView` renders it with an Open Settings button. Neither needs changing.
- Build: `xcodebuild -scheme vcdr-mac -destination 'platform=macOS' build`
- Test: `xcodebuild test -scheme vcdr-mac -destination 'platform=macOS' -quiet`

## Type

AFK

## Acceptance criteria

- [x] No event tap is created anywhere in the app, and the tap types are deleted
- [x] Hotkeys register and fire with Accessibility not granted
- [x] The first paste attempted without Accessibility prompts the user for it, once
- [x] The Home tab still reports a missing Accessibility grant as it does today
- [x] Build succeeds and the suite is green

## Blocked by

- 05-suspension-releases-every-claim
