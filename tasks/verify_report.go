package tasks

import (
	"fmt"
	"io"
	"strings"

	"github.com/glebglazov/pop/store"
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
// way splitRefinerReply peels the Refiner's `REFINE-OUTCOME:` and
// `COMMIT-SUBJECT:` lines off its document. The verdict line is pop's channel —
// the enum a drain gates on — and leaving it at the head of a document a human
// reads invites that human to take the report for the verdict, which the badge
// and the cache own.
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

// acceptedVerdictAuthorLabel heads the authorship fact of a report a human
// wrote, where a Verifier-authored one names the agent. The two documents are
// otherwise the same shape in the same directory, so this line is what a reader
// scanning the set's reports reads to tell them apart.
const acceptedVerdictAuthorLabel = "Accepted by"

// publishAcceptedVerdictReport files the human's own account of an Accepted
// verdict (ADR-0103) as the set's newest Verify report. It answers the question
// a green set with damning findings behind it puts to a later reader: the
// judgment that was overridden, the rationale for overriding it, and the fact
// that no agent was involved.
//
// No Verifier runs on this path, so there is no reply to split — pop renders the
// body from the row the human is about to write and the verdict it replaces.
// The overridden verdict must be read before the Accept upsert overwrites it at
// the same work SHA; it is nil when the set has none recorded there, which is a
// human accepting ahead of any judgment rather than over one.
//
// Filing is best-effort like the Verifier's own report: the recorded PASS is
// what the drain gates on, and an unwritable Task-set directory must not turn an
// Accept into a failure.
func publishAcceptedVerdictReport(d *Deps, out io.Writer, m *Manifest, setID, workSHA, commitRange, note string, overridden *store.VerifyVerdict) {
	if d == nil || m == nil || strings.TrimSpace(m.Dir) == "" {
		return
	}
	at := d.Now().UTC()
	doc := verifyReport.renderDocumentBy(at, setID, workSHA, commitRange,
		acceptedVerdictAuthorLabel, "human", acceptedVerdictBody(note, overridden))
	if _, err := verifyReport.writeDocument(d, m.Dir, at, doc); err != nil && out != nil {
		outputFor(out).line(ansiYellow, "   Verify report not written: %v", err)
	}
}

// acceptedVerdictBody is the prose of an Accepted verdict's report: what was
// recorded, over what, and why. The overridden verdict's own findings ride along
// because they are the reason the reader is asking — a report that named the
// verdict but dropped the findings would leave the disagreement it exists to
// explain unstated.
func acceptedVerdictBody(note string, overridden *store.VerifyVerdict) string {
	var b strings.Builder
	b.WriteString("A human recorded this PASS directly, overriding verification (ADR-0103). No Verifier judged this work SHA.\n")
	if overridden != nil && overridden.Verdict != "" && overridden.Verdict != string(VerdictPass) {
		fmt.Fprintf(&b, "\n## Overridden verdict\n\n%s\n", overridden.Verdict)
		if findings := strings.TrimSpace(overridden.Findings); findings != "" {
			fmt.Fprintf(&b, "\n%s\n", findings)
		}
	} else {
		b.WriteString("\n## Overridden verdict\n\nNone recorded at this work SHA.\n")
	}
	b.WriteString("\n## Rationale\n\n")
	if note = strings.TrimSpace(note); note != "" {
		b.WriteString(note + "\n")
	} else {
		b.WriteString("The human recorded no rationale with the Accept.\n")
	}
	return b.String()
}

// latestVerifyPointer resolves the set's current Verify report as the human
// surfaces carry it — where the document is and which commit it was written
// against, never a line of what it says — and false when the set has never been
// verified (ADR-0245).
//
// The pointer says nothing about whether the judgment still holds at HEAD: that
// is the Verified-at SHA badge's question, and a second answer to it here would
// be the second derivation ADR-0245 rejected. So a report older than HEAD is
// pointed at exactly as one written against it is.
func latestVerifyPointer(d *Deps, m *Manifest) (ReportPointer, bool) {
	return verifyReport.latestPointer(d, m)
}
