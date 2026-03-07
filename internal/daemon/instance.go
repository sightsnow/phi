package daemon

import (
	"errors"
	"path/filepath"

	"phi/internal/platform"
)

var ErrAlreadyRunning = errors.New("daemon is already running")

type instanceLock interface {
	release() error
}

func instanceLockPath(network, address string) string {
	return filepath.Join(platform.AppDir(), "daemon.lock")
}
