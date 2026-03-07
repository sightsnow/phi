//go:build windows

package daemon

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"

	"phi/internal/platform"
)

type fileInstanceLock struct {
	file *os.File
	path string
}

func acquireInstanceLock(network, address string) (instanceLock, error) {
	path := instanceLockPath(network, address)
	if err := platform.EnsureParentDir(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return &fileInstanceLock{file: file, path: path}, nil
}

func (l *fileInstanceLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, new(windows.Overlapped))
	err := l.file.Close()
	_ = os.Remove(l.path)
	return err
}
