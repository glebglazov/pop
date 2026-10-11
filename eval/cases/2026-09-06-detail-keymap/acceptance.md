# Acceptance list

Status: approved

1. Pressing `s` inside a Task set's or Map's detail view opens the Status menu over the container the detail is open on, listing the same entries `s` opens over that container's row in the row list.
2. Pressing `y` inside a detail view opens that container's Copy menu instead of copying anything outright.
3. Pressing `m` inside a detail view opens that container's Mute menu over the container the detail is open on.
4. Pressing `I` inside a detail view dispatches the same verb that the row list dispatches under `I` for that container.
5. When `I` inside the detail view of a Task set with no Worktree binding opens the drain picker, the picker shows on screen over the detail.
6. `y` inside a document peek no longer copies the item's or artifact's name, and `p` inside a peek no longer copies its path.
7. `p` does nothing in the detail's item list and in the artifact list.
8. `r` in the detail's item list, in the artifact list, and inside a peek still opens the cursored item's or artifact's own menu, unchanged.
9. Inside the detail of a container that is not `BROKEN` and whose kind offers no status, `s`, pressed when no other flash is on the detail's hint line, flashes the same "no status to write" refusal that the row list flashes, on the detail's own hint line.
10. Inside the detail of a container that is not `BROKEN` and whose kind can copy nothing, `y`, pressed when no other flash is on the detail's hint line, flashes the same "nothing to copy" refusal that the row list flashes.
11. Inside the detail of a container that is not `BROKEN` and that cannot be muted, `m`, pressed when no other flash is on the detail's hint line, flashes the same "cannot be muted" refusal that the row list flashes.
12. A menu opened from the detail of a container that is not `BROKEN` renders as the detail's own reserved block, shrinking the item or artifact list by exactly the menu's height rather than painting over it.
13. While a container menu is open over a detail, `j`/`k` move the menu cursor and Enter or an entry's letter dispatches the same verb the row-list menu dispatches.
14. `esc` closes a menu opened from a detail and leaves the detail standing.
15. A Selection marked on the row list does not make a menu opened from inside a detail plural — the menu acts on the detail's single container.
16. `/` inside a detail view opens a search over whichever list is on screen, and Enter applies it and returns the full detail keymap over the narrowed list.
17. The detail's query matches case-insensitively as a substring on an item's id, title and type, with space-separated terms AND-ed so each term must match some field but different terms may match different fields.
18. An item's status is not among the fields the detail's search reads.
19. The artifact list narrows under the same grammar when it is the list on screen, matching on the artifact's name and type.
20. `esc` while typing the detail's search abandons the edit and restores the term that was in force.
21. Reopening `/` starts from an empty buffer, so applying an empty query clears the detail's search and widens the list back to the whole container.
22. While typing the detail's search, every key but Enter, Esc and ctrl+c is entered as a character — `j`, `k`, `v` and `r` type rather than act.
23. The applied term stays in force across the poll that rebuilds the detail.
24. The detail of a container that is not `BROKEN` shows the applied term while it is in force.
25. In the detail of a container that is not `BROKEN`, a query that matches nothing shows an empty-state line naming the term and how to clear it, rather than reading as a container with no items.
26. A search hides rows without taking the Artifact view away — `v` still switches lists when a query has emptied the one on screen.
27. The detail's search buffer and applied term are separate from the row list's: closing the detail takes its query with it and leaves the row list's search as it was.
28. The hint line of the detail of a container that is not `BROKEN` names `r`, `s`, `y`, `m` and `/`, and no longer advertises a flat copy-name or copy-path key.
29. The peek's hint line advertises no flat copy-name or copy-path key.
30. In the detail of a container that is not `BROKEN`, the typing phase's hint line names only apply, cancel and help.
31. The help overlay for a detail names the run menu plus the status, copy and mute menus, the container's `I` verb when its kind offers one, and `/` search — with a clear-search entry while a term is in force — and no longer names flat copy keys; the peek's help likewise drops them.
32. The help overlay has its own arms for a menu opened over a detail and for the detail's typing phase.
