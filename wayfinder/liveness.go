package wayfinder

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/store"
)

// A claim on a Decision ticket lives exactly as long as the grilling process
// running in its owner (ADR-0193). This file is the whole of that rule: the
// store holds the rows and asks the question, and everything below is what an
// owner string means.

// ownerLiveness answers "is this claim's owner still grilling?" for the
// lifetime of one Work read. The pane half is a single whole-server listing
// memoized for that read: a Map listing overlays a claim onto every claimed
// ticket on screen, and a probe per row would fork tmux once per ticket.
//
// Its lifetime is one load by construction — a caller builds one and drops it —
// because it answers a question about a moment, and the next read must see a
// pane that has since died.
type ownerLiveness struct {
	tmux    tmux.Tmux
	pidLive func(pid int) bool

	once   sync.Once
	panes  map[string]tmux.PaneProcess
	served bool
}

// live is the predicate the store takes. It is deliberately the only exported
// shape of this type: the store compares owner strings and knows nothing about
// panes or pids.
func (l *ownerLiveness) live(owner string) bool {
	paneID, panePID, ok := parsePaneOwner(owner)
	if ok {
		return l.livePane(paneID, panePID)
	}
	if pid, ok := parsePIDOwner(owner); ok {
		return l.pidLive(pid)
	}
	// An owner shape pop does not recognise is nobody it can find a process in,
	// so it holds nothing.
	return false
}

// livePane asks the shared pane predicate about the memoized listing. No tmux
// server at all reads as no live panes: no panes means no work in flight, and
// holding every claim in the one case where the human has visibly ended
// everything is exactly the wedge liveness removes.
func (l *ownerLiveness) livePane(paneID string, panePID int) bool {
	pane, found := l.pane(paneID)
	if !livePaneCommand(pane.Command, found && l.served) {
		return false
	}
	// tmux reuses pane ids across server restarts, so an owner that recorded the
	// pane's pid is only live in the pane it actually named. An owner from before
	// the pid was recorded carries none, and is probed by the rest of the rule.
	return panePID == 0 || pane.PID == 0 || pane.PID == panePID
}

// pane reads one pane out of the memoized whole-server listing, taking the
// listing the first time anyone asks.
func (l *ownerLiveness) pane(paneID string) (tmux.PaneProcess, bool) {
	l.once.Do(func() {
		panes, err := l.tmux.AllPanes()
		l.panes, l.served = panes, err == nil
	})
	pane, found := l.panes[paneID]
	return pane, found && l.served
}

// paneOwner names a pane as a claim owner, carrying the pid tmux reports for it
// so a pane id reused after a server restart is not mistaken for this holder. A
// pane pop cannot read a pid for is named bare, which the liveness rule reads as
// "pid unknown" rather than dead.
func (l *ownerLiveness) paneOwner(paneID string) string {
	pane, _ := l.pane(paneID)
	return paneOwner(paneID, pane.PID)
}

// livePaneCommand is the one pane-liveness predicate in pop: a pane pop can
// read, running something other than a bare shell. Pane reuse asks it of one
// pane (see liveGrillingPane) and claim liveness asks it of the whole-server
// listing, so reclaiming a ticket and respawning its pane can never disagree
// about whether the session is still there.
func livePaneCommand(command string, readable bool) bool {
	return readable && !tmux.IsBareShell(command)
}

// parsePaneOwner splits a pane owner into its pane id and, when the owner
// string carries one, the pid of the pane it named.
func parsePaneOwner(owner string) (paneID string, pid int, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(owner), "pane:")
	if !found || rest == "" {
		return "", 0, false
	}
	paneID, rawPID, hasPID := strings.Cut(rest, "/")
	if !hasPID {
		return rest, 0, true
	}
	n, err := strconv.Atoi(rawPID)
	if err != nil || n <= 0 {
		return paneID, 0, true
	}
	return paneID, n, true
}

func parsePIDOwner(owner string) (int, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(owner), "pid:")
	if !found {
		return 0, false
	}
	pid, err := strconv.Atoi(rest)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// pidAlive is the `pid:` owner's probe: a zero signal, which reports whether
// the process is there without disturbing it.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// ownerLive builds the claim-liveness predicate for one read or one claim verb.
// A fresh one per call is the memo's whole scope: it forks tmux at most once,
// and never replays a listing into a later load.
func (d *Deps) ownerLive() store.OwnerLive {
	return d.ownerLiveness().live
}

// ownerLiveness builds the one pane listing a read or a claim verb gets. A claim
// verb wants the object rather than the predicate: it also names an owner, and
// that name reads the same listing.
func (d *Deps) ownerLiveness() *ownerLiveness {
	l := &ownerLiveness{tmux: d.tmux(), pidLive: pidAlive}
	if d.PIDAlive != nil {
		l.pidLive = d.PIDAlive
	}
	return l
}
