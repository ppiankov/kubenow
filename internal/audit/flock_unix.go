//go:build !windows

package audit

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// acquireFlock acquires an exclusive file lock with retries.
// Uses LOCK_EX|LOCK_NB with 100ms retry, 1s total timeout.
func acquireFlock(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR, 0644)
	if err != nil {
		return -1, fmt.Errorf("open lock file: %w", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return fd, nil
		}
		if time.Now().After(deadline) {
			unix.Close(fd)
			return -1, fmt.Errorf("flock timeout after 1s: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// releaseFlock releases the file lock and closes the fd.
func releaseFlock(fd int) {
	_ = unix.Flock(fd, unix.LOCK_UN)
	_ = unix.Close(fd)
}
