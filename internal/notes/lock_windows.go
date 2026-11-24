package notes

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on the file at path (created if
// missing) and returns its release. The lock is the handle's: another
// handle, in this process or another, waits for it, and Windows releases
// it if the process ends while holding it.
func lockFile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		windows.UnlockFileEx(h, 0, 1, 0, ol)
		f.Close()
	}, nil
}
