//go:build windows

package agent

import (
	"net"

	winio "github.com/Microsoft/go-winio"
)

func listen(endpoint string) (net.Listener, bool, error) {
	listener, err := winio.ListenPipe(endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	return listener, false, nil
}
