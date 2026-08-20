# Markdown documents render, by extension, through glamour

> **Amended by [ADR-0230](0230-pop-resolves-the-terminal-appearance-itself.md):** the automatic-style clause below is wrong wherever pop actually runs — inside tmux, glamour's automatic style always resolves to dark, whatever the terminal's background is. Pop resolves the **Terminal appearance** itself and hands glamour an explicit palette. Every other decision in this ADR stands.

The Document peek shows a file's bytes. Everything it is pointed at is Markdown written for a human to read — a task body, a Review artifact, a spec, a Routine's last report — and reading it as raw text means reading `##`, backticks and list markers as noise, in the one surface whose whole purpose is reading.

The decision: **pop renders Markdown with `github.com/charmbracelet/glamour`, and decides whether to render by file extension — a `.md` path is rendered, every other path is shown raw.** It reaches the Document peek and `pop tasks artifacts --show` when stdout is a terminal; a redirected `--show` stays byte-exact, because a document piped into a file or a pull request is not being read on a terminal. The renderer uses glamour's automatic style, takes its wrap width from the surface's own content width, and is rebuilt when the window resizes — a stale wrap width is the one glamour misconfiguration a reader sees immediately.

**Extension, not content sniffing, and no toggle.** Pop's own non-Markdown document says what it is: `progress.txt` separates its records with `---` lines, which a Markdown renderer turns into a horizontal rule per record while flattening the `HH:MM [file] outcome` headers into paragraphs — a legible file rendered into mush. Extension is a signal the documents already carry honestly, and a heuristic that guesses would get exactly this case wrong.

## Considered options

- **A small in-house renderer over lipgloss v2** — rejected. It would cover headings, emphasis, lists and fenced blocks in a few hundred lines with no new dependency, and it would mangle the first document that used a table or a nested list. The peek renders the human's own documents — ADRs, CONTEXT fragments, hand-written specs — not just pop's, so "the subset pop emits" is not the input set.
- **Render everything and accept the `progress.txt` damage** — rejected; it makes the most-read document in the set the worst-rendered one.
- **Render by extension plus a raw/rendered toggle key** — considered and dropped as unearned. One key and one bool, but nothing yet needs it; if copying a fenced block out of a review turns out to want raw bytes, `p` already copies the path.

## Consequences

- **glamour pulls `github.com/charmbracelet/lipgloss` v1 alongside pop's `charm.land/lipgloss/v2`**, plus chroma, goldmark and bluemonday — around a dozen modules. The two lipgloss majors are different module paths and coexist; the weight is real, leaf-level, and buys a CommonMark implementation instead of a growing pile of regexes.
- **A future non-Markdown artifact needs no decision.** It gets an extension, and the extension already answers the question.
- Glossary: **Document peek** is redefined.
