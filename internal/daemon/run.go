package daemon

import (
	"context"
	"errors"
	"os"

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
	return control.Serve(runCtx, listener, service)
}
