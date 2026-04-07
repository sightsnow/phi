//go:build !windows

package platform

import "errors"

func ProtectForCurrentUser(data []byte) ([]byte, error) {
	return nil, errors.New("windows secure storage is only available on windows")
}

func UnprotectForCurrentUser(data []byte) ([]byte, error) {
	return nil, errors.New("windows secure storage is only available on windows")
}
