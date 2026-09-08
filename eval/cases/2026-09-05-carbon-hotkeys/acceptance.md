# Acceptance list

Status: draft

1. Pressing the bound toggle combination while another process holds macOS Secure Event Input starts a capture, and pressing it again stops that capture.
2. The toggle combination reaches its action without any event tap taking part in the path.
3. Exactly one process-global Carbon key handler exists for the whole app, however many combinations are claimed.
4. The hotkey registry is constructed in one place only, and every hotkey owner receives it rather than making its own.
5. A claim on a key combination lasts exactly as long as the registration handle is held; releasing the handle returns that combination to the frontmost app.
6. There is no flag that leaves a registration live but inert — a claimed combination is always consumed, and an unwanted combination is released instead of disabled.
7. Re-binding the toggle shortcut in settings takes effect for the next key press, with no relaunch.
8. A registration that macOS refuses is logged together with its `OSStatus` status code instead of failing silently.
9. Shortcut display strings shown in settings render unchanged after the tap-owning type is gone.
10. A test presses the toggle combination through a registry test double and observes a capture start, on a host with no Accessibility grant.
11. Pressing the mode switcher combination opens the switcher, and pressing it again closes it.
12. Pressing the copy-latest-capture combination copies the latest capture.
13. Clearing the mode switcher shortcut or the copy-latest shortcut releases its claim, so that combination reaches the frontmost app again.
14. Re-binding the mode switcher or copy-latest shortcut takes effect for the next key press, with no relaunch.
15. Pressing the cancel combination during any active capture phase — preparing to record, recording, transcribing, executing, or awaiting preview — cancels the capture.
16. While VCDR is idle the cancel combination reaches the frontmost app unchanged.
17. Cancel's claim follows the capture phase from a single place; no other site turns cancel availability on or off.
18. Every way a capture ends releases the cancel claim, including the audio-start failure branch and the no-audio-recorded branch.
19. Re-binding the cancel shortcut during a capture leaves cancel working for the capture in flight, under the new combination.
20. A test asserts no cancel claim remains after each failure path, not only after a normal finish.
21. Pressing a destination's Activation Hotkey while recording routes that capture to that destination.
22. A destination's Activation Hotkey still fires when modifiers beyond those it binds are held, including push-to-talk modifiers.
23. Where two destinations bind the same key, the one requiring more modifiers wins for a press that satisfies both.
24. No key combination is claimed more than once at any time, and the existing binding-match rule is what decides which destination a press belongs to.
25. Activation Hotkey combinations are claimed only while recording; at every other time they reach the frontmost app.
26. Adding, re-binding or clearing a destination's Activation Hotkey takes effect for the next key press.
27. A destination binding with no modifiers is accepted rather than refused, and its breadth is logged.
28. A test drives Shift+Option+P and Option+P and observes them reaching different destinations.
29. While the shortcut recorder is open, pressing any combination VCDR binds reaches the recorder and triggers no VCDR action.
30. Closing the shortcut recorder restores exactly the claims held before it opened, and no others.
31. A capture in flight when the shortcut recorder opens still has a working cancel hotkey after the recorder closes.
32. No hotkey owner carries a suspension flag of its own; suspension is one operation on the registry.
33. Claims come back when the suspension token is released, with no caller having to reverse a flag.
34. A second suspension taken while one is still held follows a defined rule that a test pins down, rather than restoring claims early.
35. A test suspends, asserts nothing is claimed, releases, and asserts the earlier claim set is back.
36. No event tap is created anywhere in the app, and the event tap types are deleted.
37. Hotkeys register and fire on a machine where Accessibility has not been granted.
38. The first paste attempted without Accessibility prompts the user for the grant, and that is the app's only prompt.
39. The Home tab reports a missing Accessibility grant, and a missing microphone grant, exactly as before.
40. No surface mentions Secure Input, the Secure Input monitor and its test are deleted, and nothing polls the Secure Input flag.
41. The build succeeds and the test suite passes with no fewer tests than the 389-test baseline.
