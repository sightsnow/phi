package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"phi/internal/platform"
)

type Config struct {
	Sync SyncConfig `toml:"sync,omitempty"`
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
	return Config{}
}

func Load(path string) (Config, error) {
	if path == "" {
		path = platform.DefaultConfigPath()
	}
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}

	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		var strictErr *toml.StrictMissingError
		if errors.As(err, &strictErr) {
			return Config{}, fmt.Errorf("config.toml only supports [sync]; path settings are not allowed: %w", err)
		}
		return Config{}, err
	}
	return cfg, nil
}

func WriteDefault(path string) (Config, error) {
	if path == "" {
		path = platform.DefaultConfigPath()
	}
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
	return Config{}, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = platform.DefaultConfigPath()
	}
	if err := platform.EnsureParentDir(path); err != nil {
		return err
	}
	writeCfg := cfg.prepareForWrite()
	data, err := toml.Marshal(writeCfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (c Config) prepareForWrite() Config {
	out := c
	switch strings.ToLower(strings.TrimSpace(out.Sync.Backend)) {
	case "s3":
		out.Sync.WebDAV = WebDAVConfig{}
	case "webdav":
		out.Sync.S3 = S3Config{}
	}
	return out
}
