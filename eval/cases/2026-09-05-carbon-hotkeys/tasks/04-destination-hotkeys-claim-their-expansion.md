## What to build

An Output Destination's Activation Hotkey fires while recording even when
push-to-talk modifiers are held, exactly as it does today, and stops existing the
moment recording ends.

Today one tap matches every destination binding with superset modifier matching:
a binding of Option+P fires when Control+Option+Command+P is pressed, and where
two destinations share a keycode the one requiring more modifiers wins. Carbon
matches exactly, so that behaviour has to be precomputed instead of matched —
each Activation Hotkey claims its whole Modifier Expansion, the set of
combinations that include its modifiers and that no more specific Activation
Hotkey already claims.

Preserve the existing matching rule rather than reimplementing it: it is already
correct and already tested, so make it the core of the precomputation. Each
combination must be claimed at most once — Carbon refuses a duplicate
registration from the same process, and a duplicate would mean the expansion had
a bug.

A binding with no modifiers expands to every combination, including Command-C and
Command-V, for the duration of a recording. Today's tap behaves identically, so
this is not a regression and the binding is not refused; log it so it is visible.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `DestinationHotkeyManager.matchBinding` is the rule to keep. Its callers change;
  it does not.
- There are 16 modifier combinations over Control, Option, Shift and Command.
  With the maintainer's bindings (Option+P, Shift+Option+P, Option+C) the
  expansion is 8 claims on P and 8 on C.
- `destinationHotkey.isEnabled` is assigned from three places in
  `CaptureSession+Recording.swift` — armed on entering recording, released on
  stop and on cancel. Derive it from the phase alongside cancel, in the same
  place the previous slice established.
- `CaptureSession+Hotkeys.setupDestinationHotkeys()` wires the matched
  destination into `run?.destinationOverrideID` and stops the recording.
- The three destination tests in `CaptureSessionHotkeyRebindingTests` assert
  against `armedActivationHotkeys`; they become assertions about the claimed
  expansion.
- Build: `xcodebuild -scheme vcdr-mac -destination 'platform=macOS' build`
- Test: `xcodebuild test -scheme vcdr-mac -destination 'platform=macOS' -quiet`

## Type

AFK

## Acceptance criteria

- [x] Pressing a destination's Activation Hotkey while recording routes the capture to that destination
- [x] The same binding still fires when extra modifiers are held
- [x] Where two destinations share a key, the one requiring more modifiers wins, as it does today
- [x] No combination is claimed twice, and the existing matching rule is reused rather than reimplemented
- [x] Activation Hotkeys are claimed only while recording; outside a recording the combinations reach the frontmost app
- [x] Adding, re-binding or clearing a destination's Activation Hotkey takes effect for the next key press
- [x] A modifier-less binding is accepted and logged, matching today's behaviour
- [x] A test drives Shift+Option+P and Option+P to different destinations through the specificity rule
- [x] Build succeeds and the suite is green

## Blocked by

- 03-cancel-hotkey-follows-the-capture-phase
