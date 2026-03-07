//go:build !windows

package agent

import (
	"net"
	"os"
)

func listen(endpoint string) (net.Listener, bool, error) {
	_ = os.Remove(endpoint)
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, false, err
	}
	_ = os.Chmod(endpoint, 0o600)
	return listener, true, nil
}
