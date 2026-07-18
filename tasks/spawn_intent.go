package tasks

import (
	"os"
	"time"

	"github.com/glebglazov/pop/store"
)

// spawnIntentTTL bounds how long a pending-spawn marker shadows a set before it
// is treated as stale. A dispatched `pop tasks implement` reaches BeginDrain
// within seconds (it is one of the first things the process does), so this is
// generous headroom: long enough that no legitimate spawn expires before its
// running Drain row appears, short enough that a spawn which never started stops
// blocking re-selection promptly. Once BeginDrain records the running row the
// intent is deleted outright, so the TTL only governs spawns that never began.
const spawnIntentTTL = 2 * time.Minute

// PendingSpawn is a live pending-spawn marker projected at the tasks boundary
// for the queue's double-spawn guard: a set the supervisor dispatched that has
// not yet reached BeginDrain, whose recording process is still alive and whose
// intent has not expired.
type PendingSpawn struct {
	SetID       string
	RuntimePath string
}

// RecordSpawnIntent writes a durable pending-spawn marker for the set about to
// be dispatched into a pane, keyed by the repository containing runtimePath. It
// is written before the drain command is sent so a fast re-poll observing the
// store (which still shows no running Drain row until BeginDrain) treats the set
// as busy instead of dispatching a second implement. The marker carries this
// process's PID and start token so it can be reconciled if this process dies.
func RecordSpawnIntent(d *Deps, runtimePath, setID string) error {
	if setID == "" {
		return nil
	}
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return err
	}
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	pid := os.Getpid()
	procStart, _ := procStartToken(d, pid)
	return s.PutSpawnIntent(store.SpawnIntent{
		Repo:        id.CommonDir,
		SetID:       setID,
		RuntimePath: runtimePath,
		PID:         pid,
		ProcStart:   procStart,
		CreatedAt:   time.Now().UTC(),
	})
}

// PendingSpawns returns the live pending-spawn markers for the given repository
// common dir: intents still within their TTL whose recording process is alive.
// The queue folds these into its busy set so a set with a spawn in flight is not
// re-selected before its Drain row exists. It opens the store only when it
// already exists (a pure reader never materialises an empty database).
func PendingSpawns(d *Deps, repoCommonDir string) ([]PendingSpawn, error) {
	if repoCommonDir == "" {
		return nil, nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	freshAfter := time.Now().UTC().Add(-spawnIntentTTL)
	rows, err := s.SpawnIntentsForRepo(repoCommonDir, freshAfter)
	if err != nil {
		return nil, err
	}
	var out []PendingSpawn
	for _, si := range rows {
		if !drainProcessAlive(d, si.PID, si.ProcStart) {
			continue
		}
		out = append(out, PendingSpawn{SetID: si.SetID, RuntimePath: si.RuntimePath})
	}
	return out, nil
}
