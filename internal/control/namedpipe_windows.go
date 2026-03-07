//go:build windows

package control

import (
	"context"
	"net"
	"time"

	winio "github.com/Microsoft/go-winio"

	"phi/internal/platform"
)

func listenNamedPipe(address string) (net.Listener, error) {
	cfg := &winio.PipeConfig{}
	if sd := platform.CurrentUserPipeSecurityDescriptor(); sd != "" {
		cfg.SecurityDescriptor = sd
	}
	return winio.ListenPipe(address, cfg)
}

func dialControl(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
	if network == "npipe" {
		dialCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return winio.DialPipeContext(dialCtx, address)
	}
	dialer := net.Dialer{Timeout: timeout}
	return dialer.DialContext(ctx, network, address)
}
