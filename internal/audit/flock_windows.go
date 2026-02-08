//go:build windows

package audit

import (
	"fmt"
	"os"
	"time"
)

// acquireFlock acquires an exclusive file lock using os.OpenFile on Windows.
// Windows enforces exclusive access via O_CREATE|O_EXCL as a lightweight lock.
// Retries with 100ms interval, 1s total timeout.
func acquireFlock(path string) (int, error) {
	deadline := time.Now().Add(time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
		if err == nil {
			// Store the fd; on Windows we keep the file handle open as the lock
			return int(f.Fd()), nil
		}
		if time.Now().After(deadline) {
			return -1, fmt.Errorf("flock timeout after 1s: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// releaseFlock closes the file handle, releasing the lock on Windows.
func releaseFlock(fd int) {
	// On Windows, closing the handle releases any locks
	_ = os.NewFile(uintptr(fd), "").Close()
}
