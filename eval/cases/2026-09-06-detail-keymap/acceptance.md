# Acceptance list

Status: draft

1. Pressing `s` inside a Task set's or Map's detail view opens the Status menu over the container the detail is open on, listing the same entries `s` opens over that container's row in the row list.
2. Pressing `y` inside a detail view opens that container's Copy menu instead of copying anything outright.
3. Pressing `m` inside a detail view opens that container's Mute menu over the container the detail is open on.
4. Pressing `I` inside a detail view runs the container's own flat verb — the same verb the row list runs under `I` for that row.
5. `y` inside a document peek no longer copies the item's or artifact's name, and `p` inside a peek no longer copies its path.
6. `p` does nothing in the detail's item list and in the artifact list.
7. `r` in the detail's item list, in the artifact list, and inside a peek still opens the cursored item's or artifact's own menu, unchanged.
8. A container whose kind offers no status flashes the same "no status to write" refusal from inside the detail that the row list flashes, and the flash appears on the detail's own hint line.
9. A container whose kind can copy nothing flashes the same "nothing to copy" refusal from inside the detail, and a container that cannot be muted flashes the same "cannot be muted" refusal.
10. A menu opened from a detail renders as the detail's own reserved block, shrinking the item or artifact list by exactly the menu's height rather than painting over it.
11. While a container menu is open over a detail, `j`/`k` move the menu cursor and Enter or an entry's letter dispatches the same verb the row-list menu dispatches.
12. `esc` closes a menu opened from a detail and leaves the detail standing.
13. A Selection marked on the row list does not make a menu opened from inside a detail plural — the menu acts on the detail's single container.
14. `/` inside a detail view opens a search over whichever list is on screen, and Enter applies it and returns the full detail keymap over the narrowed list.
15. The detail's query matches case-insensitively as a substring on an item's id, title and type, with space-separated terms AND-ed so each term must match some field but different terms may match different fields.
16. An item's status is not among the fields the detail's search reads.
17. The artifact list narrows under the same grammar when it is the list on screen, matching on the artifact's name and type.
18. `esc` while typing the detail's search abandons the edit and restores the term that was in force.
19. Reopening `/` starts from an empty buffer, so applying an empty query clears the detail's search and widens the list back to the whole container.
20. While typing the detail's search, every key but Enter, Esc and ctrl+c is entered as a character — `j`, `k`, `v` and `r` type rather than act.
21. The applied term stays in force across the poll that rebuilds the detail, and is shown to the reader while it is in force.
22. A detail query that matches nothing shows an empty-state line naming the term and how to clear it, rather than reading as a container with no items.
23. A search hides rows without taking the Artifact view away — `v` still switches lists when a query has emptied the one on screen.
24. The detail's search buffer and applied term are separate from the row list's: closing the detail takes its query with it and leaves the row list's search as it was.
25. The detail's hint line names `r`, `s`, `y`, `m` and `/`, and no longer advertises a flat copy-name or copy-path key; the peek's hint line likewise drops them.
26. The typing phase's hint line names only apply, cancel and help.
27. The help overlay for a detail names the run menu plus the status, copy and mute menus, the container's `I` verb when its kind offers one, and `/` search — with a clear-search entry while a term is in force — and no longer names flat copy keys; the peek's help likewise drops them.
28. The help overlay has its own arms for a menu opened over a detail and for the detail's typing phase.
29. `go build ./... && go vet ./dashboard/... && go test ./dashboard/...` passes.
