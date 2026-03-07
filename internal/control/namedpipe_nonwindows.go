//go:build !windows

package control

import (
	"context"
	"errors"
	"net"
	"time"
)

func listenNamedPipe(string) (net.Listener, error) {
	return nil, errors.New("named pipe transport is only available on windows")
}

func dialControl(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	return dialer.DialContext(ctx, network, address)
}
