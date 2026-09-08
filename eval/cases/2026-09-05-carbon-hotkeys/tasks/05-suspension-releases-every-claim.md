## What to build

While the user is recording a new shortcut in settings, every VCDR hotkey lets
its combination through to the recorder, and every one of them comes back
afterwards exactly as it was.

Today this is a boolean fanned out to five owners, and a screen that forgets to
clear it leaves every hotkey dead with nothing to show why. Under Carbon the
release is real — the claims are dropped and restored — so make it a single
operation on the registry, held by a token, with restoration tied to the token
going away rather than to a caller remembering. That is what Suspension already
means in this context's language.

Each owner stops knowing about suspension entirely.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `AppDependencies.setHotkeySuppression(_:)` currently sets `isSuppressed` on the
  three `CaptureSession` hotkeys and calls `setHotkeySuppressed(_:)` on the mode
  switcher coordinator and the copy-latest handler.
- `MainWindowView` publishes it as the `hotkeySuppressionHandler` environment
  value; that is the seam the token has to travel through.
- `isSuppressed` exists on `GlobalHotkeyManager` and `DestinationHotkeyManager`
  and is documented there as orthogonal to `isEnabled`; both go.
- Restoring must not disturb each owner's own arming state — a capture in flight
  when the user opens the recorder must still have its cancel claim afterwards.
- Suspension nesting: decide and test what a second suspend while one is held
  does, rather than leaving it to chance.
- Build: `xcodebuild -scheme vcdr-mac -destination 'platform=macOS' build`
- Test: `xcodebuild test -scheme vcdr-mac -destination 'platform=macOS' -quiet`

## Type

AFK

## Acceptance criteria

- [x] While the shortcut recorder is open, pressing any bound VCDR combination reaches the recorder instead of triggering VCDR
- [x] Closing the recorder restores exactly the claims that were held before it opened, and no others
- [x] A capture in flight when the recorder opens still has a working cancel hotkey once it closes
- [x] No hotkey owner carries a suspension flag any more
- [x] Restoration is driven by the token being released, not by a caller remembering to reverse a boolean
- [x] A test suspends, asserts nothing is claimed, releases, and asserts the previous set is back
- [x] Build succeeds and the suite is green

## Blocked by

- 02-switcher-and-copy-latest-on-carbon
- 03-cancel-hotkey-follows-the-capture-phase
- 04-destination-hotkeys-claim-their-expansion
