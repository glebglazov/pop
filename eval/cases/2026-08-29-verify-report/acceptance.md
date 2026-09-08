# Acceptance list

Status: draft

1. A Verifier invocation writes a Verify report document under the set's own directory, with the instant it was written stamped into the filename.
2. Each Verifier invocation leaves its own report, so a verify-remediate-verify lap leaves one document per invocation rather than one rewritten answer.
3. The Verifier's reply keeps the machine-read verdict line first, and the prose after that line becomes the report body.
4. A malformed or absent Verifier reply yields the same verdict it yields today, and the raw reply text is still preserved rather than lost.
5. A report is written on a PASS verdict as well as on FIXABLE and NEEDS-HUMAN.
6. The Verifier response contract asks a passing run to state what it checked and why each acceptance criterion is met, and no longer instructs it to leave findings empty for a PASS.
7. A verdict served from the cache without invoking an agent writes no new report.
8. Verify reports survive verification invalidation, while the verdict cache is still deleted on invalidation.
9. The Verifier prompt never carries any previous Verify report.
10. Recording a human PASS over a non-PASS judgment writes a Verify report with no agent invoked on that path.
11. The human-authored report carries the human's own rationale and names the verdict it overrode.
12. A reader scanning the set's reports can tell a human-authored report apart from a Verifier-authored one.
13. The human-authored report files in the same directory, with the same timestamp stamp and the same header of facts as a Verifier-authored report, and the latest-report readers pick it up when it is the latest.
14. The sign-off gate's preamble names the latest Verify report and the commit it was written against, and never quotes a line of its contents.
15. The sign-off gate offers a paging entry that opens the Verify report document without leaving the prompt.
16. The set's detail view carries the Verify report pointer.
17. The Verify report appears in the set's artifact list, ranked above the Refine report.
18. No agent-facing prompt view gains the Verify report pointer, and the agent-facing prompt goldens are unmodified.
19. A set that has never been verified renders nothing at all on the gate preamble, the paging entry, the detail view and the artifact list.
20. The verified-at badge behaves exactly as before, and a report older than HEAD never makes a set count as unverified.
21. The one-line truncation of verification reasons stays where a status row has room for only one line, with the pointer to the full document beside it.
22. Refine's observable behaviour is unchanged: the same directory, the same filename stamps, the same header, and every surface it renders on looks exactly as before.
23. The refine prompt goldens are unmodified.
24. `go build ./...`, `go vet ./tasks/...` and `make test` all pass.
