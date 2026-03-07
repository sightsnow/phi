package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func AppDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".phi")
	}
	return filepath.Join(home, ".phi")
}

func DefaultConfigPath() string {
	return filepath.Join(AppDir(), "config.toml")
}

func DefaultVaultPath() string {
	return filepath.Join(AppDir(), "vault.phi")
}

func DefaultControlPath() string {
	if runtime.GOOS == "windows" {
		return "npipe://./pipe/phi-control"
	}
	return filepath.Join(AppDir(), "control.sock")
}

func EnsureParentDir(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	if isNamedPipePath(path) {
		return nil
	}
	return os.MkdirAll(filepath.Dir(path), 0o700)
}

func Exists(path string) bool {
	if path == "" || isNamedPipePath(path) {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func ControlEndpoint(path string) (network string, address string, err error) {
	if override := os.Getenv("PHI_CONTROL_ENDPOINT"); override != "" {
		return parseControlEndpoint(override)
	}
	if path == "" {
		path = DefaultControlPath()
	}
	return parseControlEndpoint(path)
}

func parseControlEndpoint(value string) (network string, address string, err error) {
	switch {
	case strings.HasPrefix(value, "unix://"):
		return "unix", strings.TrimPrefix(value, "unix://"), nil
	case strings.HasPrefix(value, "tcp://"):
		return "tcp", strings.TrimPrefix(value, "tcp://"), nil
	case strings.HasPrefix(value, "npipe://"):
		return "npipe", normalizeNamedPipePath(strings.TrimPrefix(value, "npipe://")), nil
	default:
		if runtime.GOOS == "windows" {
			return "npipe", normalizeNamedPipePath(value), nil
		}
		return "unix", value, nil
	}
}

func isNamedPipePath(value string) bool {
	return strings.HasPrefix(value, "npipe://") || strings.HasPrefix(value, `\\.\pipe\`)
}

func normalizeNamedPipePath(value string) string {
	if strings.HasPrefix(value, `\\.\pipe\`) {
		return value
	}
	value = strings.TrimPrefix(value, "./pipe/")
	value = strings.TrimPrefix(value, "/pipe/")
	value = strings.TrimPrefix(value, "pipe/")
	value = strings.TrimPrefix(value, `\\.\pipe\`)
	value = strings.TrimLeft(strings.ReplaceAll(value, "/", `\`), `\`)
	if value == "" {
		value = "phi-control"
	}
	return `\\.\pipe\` + value
}
