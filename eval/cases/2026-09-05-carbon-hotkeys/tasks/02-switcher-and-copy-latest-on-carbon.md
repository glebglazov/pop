## What to build

The mode switcher hotkey and the copy-latest-capture hotkey work through the same
Carbon registry as the toggle, so they too survive Secure Input. Both are direct
translations: one combination, one action, always claimed while the shortcut is
assigned.

The obstacle is ownership. Both types construct their own hotkey manager inline
today, which the architecture rules forbid and which leaves them with no way to
reach the shared registry. Both take it through their initialiser instead.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `ModeSwitcherCoordinator` and `CopyLatestCaptureHandler`, both under
  `vcdr-mac/Orchestration/`, each hold a privately constructed
  `GlobalHotkeyManager`.
- Both already re-arm from settings through their own `observeHotkeySettings()`,
  mirroring `CaptureSession`'s. That pattern stays; only what it drives changes.
- Both expose `setHotkeySuppressed(_:)`, called from
  `AppDependencies.setHotkeySuppression(_:)`. Leave those in place for now — a
  later slice replaces the whole suppression mechanism.
- The copy-latest hotkey is unassigned in the maintainer's current settings, so
  verifying it needs a binding set first.
- Build: `xcodebuild -scheme vcdr-mac -destination 'platform=macOS' build`
- Test: `xcodebuild test -scheme vcdr-mac -destination 'platform=macOS' -quiet`

## Type

AFK

## Acceptance criteria

- [x] Pressing the mode switcher combination opens and closes the switcher, through the registry
- [x] Pressing the copy-latest combination copies the latest capture, through the registry
- [x] Both types receive the registry through their initialiser and construct no hotkey mechanism of their own
- [x] Clearing either shortcut releases its claim, so the combination reaches the frontmost app again
- [x] Re-binding either shortcut takes effect for the next key press without a relaunch
- [x] Build succeeds and the suite is green

## Blocked by

- 01-toggle-hotkey-on-carbon
