package tasks

import (
	"crypto/rand"
	"fmt"
	"os"
	"sync"
)

// BuildVersion is the running pop binary's version string (CalVer or describe).
// Injected at build time via -ldflags and set from cmd at process start, which
// covers `go run` and a plain `go build`; empty is a legitimate value. It is the
// legible half of the derivation stamp: the executable's fingerprint is what
// separates two builds, while this says which release the stamp belongs to.
var BuildVersion string

// derivationStamp names the build whose validation rules a cached answer was
// derived under (ADR-0265 decision 1). It is a var holding a func rather than a
// plain function so a test can move the stamp without rebuilding a binary —
// which is how manifestMemo already lets a test redirect this package's cache.
//
// Resolved once and held for the life of the process (ADR-0265 decision 3): a
// daemon that re-fingerprinted its executable per access would adopt a newly
// installed build's stamp and start serving rows written under rules it does not
// run. Held from first use, an old daemon and a fresh CLI occupy different stamps
// and each serves only what its own build derived.
var derivationStamp = sync.OnceValue(resolveDerivationStamp)

// resolveDerivationStamp fingerprints the running executable: path, size and
// mtime, plus the injected version. Not a VCS revision, which collapses every
// dirty rebuild of one commit into one value — exactly the developer's inner
// loop, where validation rules change fastest (ADR-0265 decision 2).
//
// The executable is a fact about this process rather than about the files a test
// fixture lays out, so it is read through os directly rather than the filesystem
// seam; the seam a test needs is derivationStamp itself.
func resolveDerivationStamp() string {
	exe, err := os.Executable()
	if err != nil {
		return unresolvedDerivationStamp()
	}
	info, err := os.Stat(exe)
	if err != nil {
		return unresolvedDerivationStamp()
	}
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s", exe, info.Size(), info.ModTime().UnixNano(), BuildVersion)
}

// unresolvedDerivationStamp is what a process that cannot fingerprint its own
// executable stamps instead: a value nothing else can mint, so every lookup
// misses and no row it writes is ever served to another build. A fixed fallback
// string would be worse than no stamp at all — two builds would share it, which
// is the staleness the stamp exists to make unrepresentable. Cache failure is a
// miss and never a diagnostic (ADR-0243 decision 4).
func unresolvedDerivationStamp() string {
	return "unresolved\x00" + rand.Text()
}
