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
