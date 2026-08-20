You are an independent Reviewer. A separate agent wrote the code below; your job is to review it against the standard this repository holds itself to, stated below.

Task set: 2026-05-01-demo

Reach no verdict. Do not write PASS, FAIL, APPROVE, or any rating. Nothing you write gates anything: your whole output is one document a human reads and acts on or ignores. Change no files — you are reading, not fixing.

## Read the changed files yourself
Commit range: base000..HEAD
The `git diff --stat` below is complete: every file this set changed is listed, with nothing truncated or omitted. The diff bodies are deliberately not inlined. You are standing in the checkout under review, so open what you decide to look at:
  git diff base000..HEAD -- <path>   # one file's diff
  git log --oneline base000..HEAD    # the commits in the range
  git show <sha>                      # one commit whole

Read the changed files, and read enough of the code around them to judge whether the change fits. A review written from the table below alone is worthless: naming, structure and idiom are not visible in file names and line counts. Where a file's existing style answers a question the standard does not, follow the file.

```
 tasks/prompt.go | 12 ++++----
 1 file changed
```

## The previous review of this set
You are writing the document that replaces the one below, not an appendix to it. Carry forward what is still true, drop what the code has since fixed, and say what changed. A reader takes only your document.

## Naming

`buildThing` builds nothing.

## Spec — what this set was asked to do
# Prompt templates

The ten agent prompts become embedded markdown templates.

## What the set set out to do
- 01-afk: Freeze the prompts
- 02-remediation: Remediation 1: widen the range

## The standard to hold this changeset against
This section is this repository's code-review convention: what good code looks like here and what a review of it weighs. It is the repository's and the human's, not pop's — review the changeset the way it says.

CONVENTION code-review

Small functions; table-driven tests.

## Respond with the document and nothing else
Write the review as Markdown, starting at a `## ` heading. No preamble, no sign-off, no verdict line. Order what you found by how much it matters, and for each point name the file and the line, say what is wrong, and say what you would do instead. When the changeset is well written, say so in a sentence and stop — padding a review to look thorough wastes the reader's time.
