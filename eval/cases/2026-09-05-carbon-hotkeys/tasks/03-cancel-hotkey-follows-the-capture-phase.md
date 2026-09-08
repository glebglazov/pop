## What to build

The cancel hotkey exists exactly as long as there is a capture to cancel, and
never a moment longer.

This is the riskiest change in the migration and the reason it gets its own
slice. Under an event tap, "cancel is off" meant the tap saw the key and let it
through. Under Carbon there is no such state: a claimed combination is always
eaten. The maintainer's cancel binding is Shift+Tab — an ordinary key combination
that apps use for reverse-tab navigation — so a claim left standing while VCDR
sits idle would swallow it system-wide. That is a worse bug than the one being
fixed.

The fix is not diligence. Cancel's availability is already a pure function of the
capture phase, so derive the claim from the phase in the single place the phase
changes, and delete the scattered assignments that each represent a chance to
forget one. The error paths are where a forgotten assignment would hide.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `cancelHotkey.isEnabled` is assigned from six places across
  `CaptureSession.swift`, `CaptureSession+Hotkeys.swift` and
  `CaptureSession+Recording.swift`. All of them go.
- `updateCancelHotkey()` in `CaptureSession+Hotkeys.swift` already encodes the
  rule: cancel is live in `preparingToRecord`, `recording`, `transcribing`,
  `executing` and `awaitingPreview`. It collapses into the phase transition.
- `setStatus(_:)` on `CaptureSession` is the single point every phase change
  passes through.
- The failure branches in `CaptureSession+Recording.swift` — audio start failing,
  no audio recorded — are the ones most likely to strand a claim; they are the
  interesting test cases, not the happy path.
- `CaptureSessionHotkeyRebindingTests` has a test asserting cancel stays live
  when the shortcut is re-bound mid-capture. It must keep passing, expressed
  against the live registration rather than a boolean.
- Build: `xcodebuild -scheme vcdr-mac -destination 'platform=macOS' build`
- Test: `xcodebuild test -scheme vcdr-mac -destination 'platform=macOS' -quiet`

## Type

AFK

## Acceptance criteria

- [x] Pressing the cancel combination during any active capture phase cancels the capture
- [x] While VCDR is idle the cancel combination reaches the frontmost app unchanged
- [x] The claim is derived from the capture phase in one place; no site assigns cancel availability directly
- [x] Every exit path releases the claim, including the audio-start failure and no-audio-recorded branches
- [x] Re-binding the cancel shortcut mid-capture leaves cancel live for the capture in flight
- [x] A test asserts the claim is gone after each failure path, not only after a normal finish
- [x] Build succeeds and the suite is green

## Blocked by

- 01-toggle-hotkey-on-carbon
