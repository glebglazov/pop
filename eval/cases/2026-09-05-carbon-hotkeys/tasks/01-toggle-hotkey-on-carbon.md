## What to build

The toggle recording hotkey keeps working while another process holds macOS
Secure Event Input. Today every VCDR hotkey runs through a session event tap, and
while any process on the machine holds Secure Input macOS silently stops
delivering key events to every such tap — the tap still reports itself installed,
nothing is logged, and the user just presses the key and gets nothing. A Carbon
hotkey survives that, measured on the maintainer's machine.

This slice introduces the mechanism and moves exactly one hotkey onto it, so the
whole design is proven end to end before the riskier owners follow. The other
hotkeys keep their event taps for now; the two mechanisms coexist deliberately.

The mechanism is a registry that owns the one process-global Carbon handler and
hands out registration handles. Holding a handle *is* holding the claim on a key
combination — releasing it returns the combination to the frontmost app. There is
no enabled/disabled boolean, because Carbon has no pass-through: a registered
combination is always consumed. A test double for the registry lets tests drive a
real key press through to its effect, which the event tap never allowed because
installing one needs an Accessibility grant the test host does not have.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `vcdr-mac/Hotkeys/` is where the new registry, handle, suspension token and
  Carbon modifier translation belong, beside the existing `EventTapManager`.
- `GlobalHotkeyManager` currently owns both the tap and the per-shortcut state.
  Its `shortcutDisplayString` / `keyNameForCode` statics are used by
  `HotkeyBinding.displayString` in `AppSettings.swift`, so they must move to
  `HotkeyBinding` before the type can be deleted in a later slice.
- `AppDependencies` is the only place objects are constructed (architecture rule
  2). The registry is created there and injected.
- `CaptureSession` holds `toggleHotkey`, `cancelHotkey` and `destinationHotkey`;
  `CaptureSession+Hotkeys.swift` registers them from settings and re-arms them
  through `observeHotkeySettings()`.
- Carbon modifier constants are `cmdKey`, `shiftKey`, `optionKey`, `controlKey`;
  `HotkeyBinding.modifiers` stores `CGEventFlags` raw bits, so a translation is
  needed. Extract them as named constants — architecture rule 3 forbids the bare
  literals.
- The Carbon handler runs synchronously on the main thread inside
  `NSApplication.run()`'s dispatch. The existing comments in `GlobalHotkeyManager`
  about never hopping through GCD or a Task apply unchanged and should carry over.
- `CaptureSessionHotkeyRebindingTests` currently asserts what a tap *targets*
  rather than what is armed, and says so in its own header comment. With an
  injected registry it can assert the live registration set instead.
- Build: `xcodebuild -scheme vcdr-mac -destination 'platform=macOS' build`
- Test: `xcodebuild test -scheme vcdr-mac -destination 'platform=macOS' -quiet`
  (baseline per the recent log: 389 tests in 65 suites, exit 0)

## Type

AFK

## Acceptance criteria

- [x] Pressing the bound toggle combination starts and stops a capture, with no event tap involved in that path
- [x] The registry owns exactly one Carbon event handler for the whole app and is constructed only in `AppDependencies`
- [x] Releasing a registration handle returns the combination to the frontmost app; there is no boolean that enables or disables a live registration
- [x] Re-binding the toggle shortcut in settings takes effect for the next key press without a relaunch
- [x] A failed registration is logged with its `OSStatus` rather than passing silently
- [x] Shortcut display strings no longer depend on the tap-owning type
- [x] A test drives a toggle key press through the injected test double to a started capture, needing no Accessibility grant
- [x] Build succeeds and the suite is green with no fewer tests than the baseline

## Blocked by

- None - can start immediately
