//go:build !windows

package daemon

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"

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
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
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
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	err := l.file.Close()
	_ = os.Remove(l.path)
	return err
}
