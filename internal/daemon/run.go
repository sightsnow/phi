package daemon

import (
	"context"
	"os"

	"phi/internal/control"
	"phi/internal/platform"
)

func Run(ctx context.Context, controlPath, vaultPath string) error {
	network, address, err := platform.ControlEndpoint(controlPath)
	if err != nil {
		return err
	}
	if network == "unix" {
		if err := platform.EnsureParentDir(address); err != nil {
			return err
		}
	}
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
		VaultPath:      vaultPath,
	}, cancel)
	return control.Serve(runCtx, listener, service)
}
