package errand

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// Unix socket paths have a short platform limit. A store-keyed directory under
// /tmp also keeps a long XDG_DATA_HOME usable; only this user can enter it.
func wakePath(td *tasks.Deps) string {
	key := sha256.Sum256([]byte(tasks.DrainStorePathWith(td)))
	return filepath.Join("/tmp", fmt.Sprintf("pop-errands-%d-%x", os.Getuid(), key[:12]), "wake.sock")
}

func wake(td *tasks.Deps) {
	c, err := net.DialTimeout("unixgram", wakePath(td), 50*time.Millisecond)
	if err != nil {
		return
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	_, _ = c.Write([]byte{1})
}

// Listen opens the wake channel while the caller holds the supervisor lock.
// Notifications coalesce; SQLite remains the source of pending subjects.
func Listen(td *tasks.Deps) (<-chan struct{}, func(), error) {
	path := wakePath(td)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	c, err := net.ListenPacket("unixgram", path)
	if err != nil {
		return nil, nil, err
	}
	wakes := make(chan struct{}, 1)
	go func() {
		var buf [1]byte
		for {
			if _, _, err := c.ReadFrom(buf[:]); err != nil {
				return
			}
			select {
			case wakes <- struct{}{}:
			default:
			}
		}
	}()
	return wakes, func() { _ = c.Close(); _ = os.Remove(path); _ = os.Remove(filepath.Dir(path)) }, nil
}
