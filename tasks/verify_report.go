package tasks

import (
	"io"
	"strings"
)

// verifyReport is verification's report family on the shared pass-report
// machinery (ADR-0245): one document per Verifier invocation, named
// `verify-<instant>.md` under the set's own `verify/` directory, headed by the
// facts a reader needs to know which tree was judged.
//
// The directory is a log where `verify_verdicts` is a cache. The cache is keyed
// by work SHA and is deleted outright when verification is invalidated; these
// documents survive that deletion, which is what makes them the durable answer
// to "why did verification judge as it did" that the cache never was. The two
// can therefore disagree about how many verdicts a set has had, deliberately.
var verifyReport = passReport{
	ArtifactType: ArtifactTypeVerify,
	DirName:      VerifyDirName,
	FilePrefix:   "verify-",
	Title:        "Verify report",
	WrittenLabel: "Verified",
	AgentLabel:   "Verifier",
	Noun:         "verify",
	PointerLabel: "Verify",
}

// splitVerifierReply peels the machine-read verdict line off the front of the
// Verifier's reply and returns the prose that is left as the report body, the
// way splitRefinerReply peels the Refiner's `COMMIT-SUBJECT:` line off its
// document. The verdict line is pop's channel — the enum a drain gates on — and
// leaving it at the head of a document a human reads invites that human to take
// the report for the verdict, which the badge and the cache own.
//
// The rule for what counts as the verdict line is ParseVerdict's own, so the
// document and the stored verdict can never disagree about which line was
// lifted. Two replies keep their whole text: one pop could not parse into a
// verdict, since that raw text is the only account of what the Verifier said,
// and one that is nothing but its verdict line, since an empty document would
// record less than the line it dropped.
func splitVerifierReply(raw string) string {
	trimmed := strings.TrimSpace(raw)
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		value, ok := verdictLabelValue(line)
		if !ok {
			continue
		}
		if _, ok := canonicalVerdict(value); !ok {
			break
		}
		body := strings.TrimSpace(strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n"))
		if body == "" {
			break
		}
		return body
	}
	return trimmed
}

// publishVerifyReport files the Verifier's reasoning as the set's newest Verify
// report. Only a real invocation reaches here: a
// verdict served from the cache re-uses reasoning that was already published,
// and writing it again would claim a judgment that never happened.
//
// Every verdict is published, PASS included (ADR-0245). A green set is the one
// a reader can least reconstruct — the old response contract recorded nothing
// at all for it — so it is the case this buys the extra output tokens for.
//
// Filing is best-effort, like the Captured run beside it: the verdict is what
// gates the drain, and an unwritable Task-set directory must not turn a PASS
// into a failed verification. The operator is told the document is missing and
// the run goes on.
func publishVerifyReport(d *Deps, out io.Writer, m *Manifest, setID, workSHA, commitRange, agent, raw string) {
	if d == nil || m == nil || strings.TrimSpace(m.Dir) == "" {
		return
	}
	body := splitVerifierReply(raw)
	if body == "" {
		// A Verifier that answered with nothing at all is exactly why the verdict
		// is NEEDS-HUMAN, so the document says that rather than standing empty and
		// leaving a reader to guess whether the pass ran.
		body = unparsedFindings(raw)
	}
	at := d.Now().UTC()
	doc := verifyReport.renderDocument(at, setID, workSHA, commitRange, agent, body)
	if _, err := verifyReport.writeDocument(d, m.Dir, at, doc); err != nil && out != nil {
		outputFor(out).line(ansiYellow, "   Verify report not written: %v", err)
	}
}
