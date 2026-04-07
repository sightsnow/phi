//go:build !windows

package platform

import "errors"

func SetLaunchAtLogin(command string) error {
	return errors.New("windows login autostart is only available on windows")
}

func DisableLaunchAtLogin() error {
	return errors.New("windows login autostart is only available on windows")
}

func LaunchAtLoginEnabled() (bool, string, error) {
	return false, "", errors.New("windows login autostart is only available on windows")
}

func DaemonAutoStartCommand(exe string) string {
	return exe
}
