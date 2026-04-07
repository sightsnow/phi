//go:build windows

package platform

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsRunKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	windowsRunValueName = "phi"
)

func SetLaunchAtLogin(command string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(windowsRunValueName, strings.TrimSpace(command))
}

func DisableLaunchAtLogin() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()
	err = key.DeleteValue(windowsRunValueName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

func LaunchAtLoginEnabled() (bool, string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, "", nil
		}
		return false, "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(windowsRunValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, "", nil
		}
		return false, "", err
	}
	return true, value, nil
}

func FormatWindowsCommandArg(arg string) string {
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}

func formatPowerShellSingleQuoted(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", "''") + "'"
}

func DaemonAutoStartCommand(exe string) string {
	return fmt.Sprintf(
		`powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -Command "Start-Process -WindowStyle Hidden -FilePath %s -ArgumentList %s"`,
		formatPowerShellSingleQuoted(exe),
		formatPowerShellSingleQuoted("__daemon"),
	)
}
