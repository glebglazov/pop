---
fragment: 3A6E2F74
generation: 0016
branch: master
---

~ Config merge order
  How pop resolves effective configuration, by a **scope-first** law: the most specific scope wins, and within one scope a Declaration (hand-authored or committed) beats a **Config gap-filler**. Ladder, most specific first: repository-scope declarations (`config.toml [repo."<path>"]`, then `.pop/config.toml` at this worktree, then the **Trunk worktree**'s) → repository-scope gap-fillers → global declarations (`config.toml`, `~/.agents/docs/<kind>.md`) → global gap-fillers → embedded default. An **Override config layer** is *not* a rank in this ladder: it is orthogonal, laid over whatever the ladder produced, and always wins. For an agent-list key a per-invocation `--agent` flag stays above even that.
  was: How pop resolves effective configuration, by an ownership/modality-first law: (1) hand-authored config always beats runtime-generated config at any scope; (2) the user's central config.toml beats a repo's in-tree .pop/config.toml; (3) the Override config layer sits above the whole hand-authored tier. Ladder, highest→lowest: config.override.toml → config.toml [repo."<path>"] → config.toml global → this worktree's .pop/config.toml → the Trunk worktree's .pop/config.toml → runtime (config.runtime.toml) → embedded default. Runtime is a gap-filler: to override it, remove or edit the hand-authored value.
  under: Configuration

~ Override config layer
  Where pop records what a human deliberately **stated** they want, scoped (checkout, repository or global) and laid orthogonally over the whole **Config merge order** rather than ranked inside it — so a human can always win, which is what lets the ladder below be ordered purely by specificity. Every leaf key is an **Overridable key** by default. The unit is one key's entire value, never a patch of it; for a **Repo convention** the unit is prose, layered on top of the composed stack rather than replacing it. Removing an override restores the resolved value, which is not the same as overriding to an empty one.
  was: The second pop-written config file, $XDG_DATA_HOME/pop/config.override.toml, holding whole-key values a human deliberately overrode — and the one layer ranked *above* every hand-authored source (ADR-0202). Opposite rank to Integration runtime config, deliberately a separate file so neither carries two contradictory ranks. The unit is one key's entire value, never a patch of it. Global-scoped, so an override travels with neither a machine nor a clone.
  avoid: top-ranked layer, session-lived promotion, per-key opt-in exposure
  under: Configuration

+ Config gap-filler
  A pop-written value that **records what happened** rather than what a human stated — pop's own derivation, or a surface's last pick. It sits at the bottom of its scope in the **Config merge order**, below every Declaration, because a record is not a claim. The counterpart of an **Override config layer** entry, and the cut between them is intent-versus-record, not who wrote the file: `pop config repo set`, `--trunk` and `--no-<component>` all state intent and belong in the override layer despite being pop-written. **Convention memory** is the one clear gap-filler pop holds.
  avoid: runtime layer, scratch, pop-written (as a synonym for either half)
  under: Configuration

- Override-exposed key

+ Overridable key
  Any config leaf a human may override, which is every leaf by default — the inverse of the opt-in tag it replaces. A key is marked *not* overridable only when it shapes **where config comes from** instead of holding a value: `includes`, which selects the files that merge, and `repo`, which is a scope selector spelled as a table rather than a table of values. Tables are never override units; only leaves are.
  avoid: exposed key, override tag scope, structural key
  under: Configuration

~ Trunk worktree
  A repository's single canonical fork base for managed **Worktree set**s, held as a **repository-scoped** path value — `trunk = "<path>"` — not as a boolean marking one checkout's block. A non-bare repo defaults to the git main worktree with no config; a bare repo has none until one is named, and without one pop can only drain in place. The trunk also roots each **Map session**. Naming it is a statement, so it lands in the **Override config layer** like any other repository setting, whether written by `--trunk` or edited in the **Config dashboard**.
  was: A repository's single canonical fork base for managed Worktree sets. A non-bare repo defaults its trunk to the git main worktree with no config; a bare repo has no implicit trunk and must have one named, either hand-authored as a `trunk = true` per-checkout Repo override or recorded by pop into the runtime tier (config.runtime.toml) when --trunk names one at a managed register — the hand-authored value winning, and the trunk key itself never resolving through the trunk-anchored runtime layer.
  avoid: trunk = true, per-checkout trunk marker, checkout scope
  under: Configuration

~ Config dashboard
  The human's editor for every **Overridable key** and every **Repo convention** — one searchable list, **Contested key**s sorted to the top, and a pane showing what is in force, which layer produced it and what it stands on. It is the *only interactive* writer of the **Override config layer**, not the only writer: a non-interactive twin (`--trunk`, `--no-<component>`, `pop config repo set`) writes the same layer through the same validation gate, because a bare repo's first register and a scripted integrate cannot open a TUI. What the model forbids is a second *destination*, not a second front-end.
  was: The one surface for Override-exposed keys: a searchable list of their dotted paths on the left, a config-format preview of the highlighted key on the right (ADR-0202). It is also the only writer of the override layer: enter opens $EDITOR in place on the whole key = value line in force, ctrl+y copies the source value down, ctrl+x removes the override.
  avoid: the only writer, third dashboard page, separate conventions dashboard
  under: Configuration

+ Contested key
  A key more than one layer of the **Config merge order** holds a value for. Contested keys sort to the top of the **Config dashboard**'s list and carry a marker, because "what did I customize, and what is quietly fighting it" is the question the surface exists to answer.
  avoid: conflict, shadowed key, overridden key
  under: Configuration
