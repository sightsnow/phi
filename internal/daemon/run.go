package daemon

import (
	"context"
	"errors"
	"os"
	"strings"

	"phi/internal/control"
	"phi/internal/platform"
)

func Run(ctx context.Context) error {
	network, address, err := platform.ControlEndpoint(platform.DefaultControlPath())
	if err != nil {
		return err
	}
	if network == "unix" {
		if err := platform.EnsureParentDir(address); err != nil {
			return err
		}
	}
	lock, err := acquireInstanceLock(network, address)
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			return nil
		}
		return err
	}
	defer lock.release()

	listener, err := control.Listen(network, address)
	if err != nil {
		return err
	}
	defer listener.Close()
	if network == "unix" {
		defer os.Remove(address)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	service := NewService(Options{
		PID:            os.Getpid(),
		ControlNetwork: network,
		ControlAddress: address,
	}, cancel)
	if err := autoUnlockIfConfigured(runCtx, service); err != nil {
		return err
	}
	return control.Serve(runCtx, listener, service)
}

func autoUnlockIfConfigured(ctx context.Context, service *Service) error {
	path := platform.DefaultWindowsAutoUnlockPath()
	if !platform.Exists(path) {
		return nil
	}
	encrypted, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return service.AutoUnlock(ctx, strings.TrimSpace(string(encrypted)))
}
