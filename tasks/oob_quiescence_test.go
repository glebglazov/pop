package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// seedLiveDrain inserts a running drain row on runtimePath owned by pid/token,
// so a quiescence test can make the checkout non-idle.
func seedLiveDrain(t *testing.T, d *Deps, runtimePath, setID string, pid int, token string) {
	t.Helper()
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = d.CloseStore() }()
	if _, err := s.StartDrain(store.Drain{
		Repo: "/repo/.git", SetID: setID, RuntimePath: runtimePath,
		PID: pid, ProcStart: token, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
}

// seedGateHold registers a Checkout gate hold on runtimePath owned by pid/token.
func seedGateHold(t *testing.T, d *Deps, runtimePath, setID string, pid int, token string) {
	t.Helper()
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = d.CloseStore() }()
	if err := s.PutCheckoutGateHold(store.CheckoutGateHold{
		RuntimePath: runtimePath, SetID: setID, PID: pid, ProcStart: token,
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
}

// aliveSeam wires ProcessAlive/ProcessStartToken so pid reads live with token.
func aliveSeam(d *Deps, livePID int, token string) {
	d.ProcessAlive = func(pid int) bool { return pid == livePID }
	d.ProcessStartToken = func(pid int) (string, bool) { return token, true }
}

// TestAcceptRefusedByLiveDrain: `--accept` is refused with an occupant-naming
// error while a live drain runs on the checkout, and writes no verdict (ADR-0104).
func TestAcceptRefusedByLiveDrain(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaACC\n", "", ""))
	seedLiveDrain(t, d, "/rt", "otherset", 4242, "tok")
	aliveSeam(d, 4242, "tok")

	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Accept: true, AcceptNote: "human ok",
		runVerifier: func(string) (string, error) { t.Fatal("must not verify"); return "", nil },
	})
	if err == nil {
		t.Fatal("accept must be refused while a live drain runs")
	}
	if !strings.Contains(err.Error(), "drain is running") || !strings.Contains(err.Error(), "otherset") {
		t.Fatalf("error must name the live drain occupant: %v", err)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaACC"); stored != nil {
		t.Fatalf("no verdict must be written on refusal, got %+v", stored)
	}
}

// TestRemediateRefusedByLiveDrain: `--remediate` is refused while a live drain
// runs, and appends no Remediation task (ADR-0104).
func TestRemediateRefusedByLiveDrain(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaR\n", "", ""))
	seedLiveDrain(t, d, "/rt", "otherset", 4242, "tok")
	aliveSeam(d, 4242, "tok")

	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Remediate: true, RemediateNote: "fix it",
		runVerifier: func(string) (string, error) { t.Fatal("must not verify"); return "", nil },
	})
	if err == nil {
		t.Fatal("remediate must be refused while a live drain runs")
	}
	if !strings.Contains(err.Error(), "drain is running") {
		t.Fatalf("error must name the live drain: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(defPath, "demo", "02-remediation.md")); !os.IsNotExist(statErr) {
		t.Fatalf("no remediation task must be written on refusal")
	}
}

// TestAcceptRefusedByLiveGateHold: `--accept` is refused while a Checkout gate
// hold is registered by a live process.
func TestAcceptRefusedByLiveGateHold(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaACC\n", "", ""))
	seedGateHold(t, d, "/rt", "gatedset", 777, "gtok")
	aliveSeam(d, 777, "gtok")

	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Accept: true, AcceptNote: "human ok",
	})
	if err == nil {
		t.Fatal("accept must be refused while a live gate hold is registered")
	}
	if !strings.Contains(err.Error(), "verification gate") || !strings.Contains(err.Error(), "gatedset") {
		t.Fatalf("error must name the gate-hold occupant: %v", err)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaACC"); stored != nil {
		t.Fatalf("no verdict must be written on refusal, got %+v", stored)
	}
}

// TestAcceptProceedsPastDeadDrain: a dead-owner drain row does not block the
// mutation (liveness respected).
func TestAcceptProceedsPastDeadDrain(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaACC\n", "", ""))
	seedLiveDrain(t, d, "/rt", "otherset", 4242, "tok")
	// The recorded owner is dead: nothing reads alive.
	d.ProcessAlive = func(int) bool { return false }
	d.ProcessStartToken = func(int) (string, bool) { return "", false }

	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Accept: true, AcceptNote: "human ok",
	})
	if err != nil {
		t.Fatalf("accept past a dead drain: %v", err)
	}
	if res.Verdict != VerdictPass {
		t.Fatalf("verdict = %q, want PASS", res.Verdict)
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaACC")
	if stored == nil || !stored.HumanAuthored {
		t.Fatalf("human PASS should be committed past a dead drain, got %+v", stored)
	}
}

// TestAcceptProceedsPastDeadGateHold: an orphan (dead-owner) gate hold does not
// block the mutation.
func TestAcceptProceedsPastDeadGateHold(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaACC\n", "", ""))
	seedGateHold(t, d, "/rt", "gatedset", 777, "gtok")
	d.ProcessAlive = func(int) bool { return false }
	d.ProcessStartToken = func(int) (string, bool) { return "", false }

	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Accept: true, AcceptNote: "human ok",
	}); err != nil {
		t.Fatalf("accept past a dead gate hold: %v", err)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaACC"); stored == nil {
		t.Fatalf("human PASS should be committed past a dead gate hold")
	}
}
