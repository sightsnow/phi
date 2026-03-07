//go:build windows

package agent

import (
	"net"

	winio "github.com/Microsoft/go-winio"

	"phi/internal/platform"
)

func listen(endpoint string) (net.Listener, bool, error) {
	cfg := &winio.PipeConfig{}
	if sd := platform.CurrentUserPipeSecurityDescriptor(); sd != "" {
		cfg.SecurityDescriptor = sd
	}
	listener, err := winio.ListenPipe(endpoint, cfg)
	if err != nil {
		return nil, false, err
	}
	return listener, false, nil
}
