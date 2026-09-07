# Assist panes are addressed by slot, and a container may hold nine

Assist was the one supervised activity a human opens in order to talk, and it
was capped at one pane per container: `LaunchAssist` and `SpawnAssist` both
looked up `@pop_assist = <container-id>`, jumped to whatever pane they found,
and spawned only when there was none. One conversation per Task set and per Map
is the wrong cap for the thing assist actually is — a second question about the
same set arrives while the first session is still mid-answer — so a container
may now hold up to nine assist panes, and both assist verbs on the **Work
dashboard** open the **Assist pane menu** rather than launching.

## The slot, not the tag value

Each pane carries an **Assist pane slot** — a digit 1–9 on a per-pane option of
its own — while `@pop_assist` keeps holding the container id for every pane in
the set. The tag value is read elsewhere as *which container this pane belongs
to*: `setkind.AttributePane` seeds the dashboard cursor from it and the row's
`IVFA` activity cluster keys its liveness on it. Encoding plurality into the
value (`<set-id>#2`) would have needed no new option and would have broken both
of those silently, so the discriminator went beside the tag instead of into it.

The digit *is* the slot rather than a position in the list. A new pane takes the
lowest free slot, so `3` reaches the same session for as long as that pane
lives, and closing a pane leaves a digit unlisted instead of renumbering its
siblings under the operator's fingers. Nine slots is the whole range: a tenth
pane would be a session no digit could address, so it is refused.

## What the cap was holding up

For a Task set the cap was only ergonomic. An **Assist session** claims nothing,
and each of its tree-touching verbs takes the **Checkout claim** with an
**Admission wait** when it is chosen, so two sessions on one set already
serialise where it matters and nothing had to change to make them safe.

For a Map it was load-bearing, and this decision accepts the loss. `SpawnAssist`
documented the single reused pane as what "dissolves the two-sessions-editing-
one-map race": manifest writes take the per-Map lock, but **Map assist**'s prose
edits to `map.md` outside the `pop:generated` markers take nothing. With two
assist panes live on one Map, those edits are last-writer-wins. The alternative
— extending plurality to Task sets only — was considered and rejected: the same
pressure produces the same second conversation on a Map, and a claim row for a
session that holds no ticket would need a TTL and a release path for a pane that
can simply die.
