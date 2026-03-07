package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"phi/internal/platform"
)

type Config struct {
	VaultPath string        `toml:"vault_path,omitempty"`
	Control   ControlConfig `toml:"control,omitempty"`
	Sync      SyncConfig    `toml:"sync,omitempty"`
}

type ControlConfig struct {
	Path string `toml:"path,omitempty"`
}

type SyncConfig struct {
	Backend string       `toml:"backend,omitempty"`
	S3      S3Config     `toml:"s3,omitempty"`
	WebDAV  WebDAVConfig `toml:"webdav,omitempty"`
}

type S3Config struct {
	Endpoint        string `toml:"endpoint,omitempty"`
	Region          string `toml:"region,omitempty"`
	Bucket          string `toml:"bucket,omitempty"`
	Prefix          string `toml:"prefix,omitempty"`
	AccessKeyID     string `toml:"access_key_id,omitempty"`
	SecretAccessKey string `toml:"secret_access_key,omitempty"`
}

type WebDAVConfig struct {
	Endpoint string `toml:"endpoint,omitempty"`
	Root     string `toml:"root,omitempty"`
	Username string `toml:"username,omitempty"`
	Password string `toml:"password,omitempty"`
}

func Default() Config {
	return DefaultForPath(platform.DefaultConfigPath())
}

func DefaultForPath(path string) Config {
	if path == "" || samePath(path, platform.DefaultConfigPath()) {
		return Config{
			VaultPath: platform.DefaultVaultPath(),
			Control: ControlConfig{
				Path: platform.DefaultControlPath(),
			},
			Sync: SyncConfig{},
		}
	}

	baseDir := filepath.Dir(path)
	controlPath := platform.DefaultControlPath()
	if runtime.GOOS != "windows" {
		controlPath = filepath.Join(baseDir, "control.sock")
	}
	return Config{
		VaultPath: filepath.Join(baseDir, "vault.phi"),
		Control: ControlConfig{
			Path: controlPath,
		},
		Sync: SyncConfig{},
	}
}

func Load(path string) (Config, error) {
	if path == "" {
		path = platform.DefaultConfigPath()
	}
	cfg := DefaultForPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults(path)
	return cfg, nil
}

func WriteDefault(path string) (Config, error) {
	if path == "" {
		path = platform.DefaultConfigPath()
	}
	cfg := DefaultForPath(path)
	if err := platform.EnsureParentDir(path); err != nil {
		return Config{}, err
	}

	data, err := toml.Marshal(Config{})
	if err != nil {
		return Config{}, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = platform.DefaultConfigPath()
	}
	if err := platform.EnsureParentDir(path); err != nil {
		return err
	}
	writeCfg := cfg.prepareForWrite(path)
	data, err := toml.Marshal(writeCfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (c Config) prepareForWrite(path string) Config {
	defaults := DefaultForPath(path)
	out := c
	if samePath(out.VaultPath, defaults.VaultPath) {
		out.VaultPath = ""
	}
	if samePath(out.Control.Path, defaults.Control.Path) {
		out.Control.Path = ""
	}
	switch strings.ToLower(strings.TrimSpace(out.Sync.Backend)) {
	case "s3":
		out.Sync.WebDAV = WebDAVConfig{}
	case "webdav":
		out.Sync.S3 = S3Config{}
	}
	return out
}

func (c *Config) applyDefaults(path string) {
	defaults := DefaultForPath(path)
	if c.VaultPath == "" {
		c.VaultPath = defaults.VaultPath
	}
	if c.Control.Path == "" {
		c.Control.Path = defaults.Control.Path
	}
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
