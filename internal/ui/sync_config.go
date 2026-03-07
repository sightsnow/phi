package ui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"phi/internal/config"
)

func PromptSyncConfig(existing config.SyncConfig) (config.SyncConfig, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return config.SyncConfig{}, errors.New("interactive sync config requires a terminal")
	}

	reader := bufio.NewReader(os.Stdin)
	backend := detectSyncBackend(existing)

	fmt.Fprintln(os.Stdout, "Configure sync backend:")
	fmt.Fprintln(os.Stdout, "  1) webdav")
	fmt.Fprintln(os.Stdout, "  2) s3")

	selectedBackend, err := promptBackend(reader, backend)
	if err != nil {
		return config.SyncConfig{}, err
	}

	syncCfg := config.SyncConfig{Backend: selectedBackend}
	switch selectedBackend {
	case "webdav":
		webdav, err := promptWebDAVConfig(reader, existing.WebDAV)
		if err != nil {
			return config.SyncConfig{}, err
		}
		syncCfg.WebDAV = webdav
	case "s3":
		s3cfg, err := promptS3Config(reader, existing.S3)
		if err != nil {
			return config.SyncConfig{}, err
		}
		syncCfg.S3 = s3cfg
	default:
		return config.SyncConfig{}, fmt.Errorf("unsupported backend %q", selectedBackend)
	}

	return syncCfg, nil
}

func detectSyncBackend(existing config.SyncConfig) string {
	backend := strings.ToLower(strings.TrimSpace(existing.Backend))
	if backend == "s3" || backend == "webdav" {
		return backend
	}
	if existing.S3.Endpoint != "" || existing.S3.Bucket != "" {
		return "s3"
	}
	if existing.WebDAV.Endpoint != "" {
		return "webdav"
	}
	return ""
}

func promptBackend(reader *bufio.Reader, current string) (string, error) {
	for {
		value, err := prompt(reader, "Backend", current)
		if err != nil {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "webdav":
			return "webdav", nil
		case "2", "s3":
			return "s3", nil
		case "":
			if strings.TrimSpace(current) != "" {
				return current, nil
			}
			fmt.Fprintln(os.Stdout, "Backend is required.")
		default:
			fmt.Fprintln(os.Stdout, "Invalid backend, use webdav or s3.")
		}
	}
}

func promptWebDAVConfig(reader *bufio.Reader, current config.WebDAVConfig) (config.WebDAVConfig, error) {
	endpoint, err := promptRequired(reader, "WebDAV endpoint", current.Endpoint)
	if err != nil {
		return config.WebDAVConfig{}, err
	}
	root, err := prompt(reader, "WebDAV root", current.Root)
	if err != nil {
		return config.WebDAVConfig{}, err
	}
	username, err := prompt(reader, "WebDAV username", current.Username)
	if err != nil {
		return config.WebDAVConfig{}, err
	}
	password, err := promptKeep(reader, "WebDAV password", current.Password)
	if err != nil {
		return config.WebDAVConfig{}, err
	}
	return config.WebDAVConfig{
		Endpoint: endpoint,
		Root:     root,
		Username: username,
		Password: password,
	}, nil
}

func promptS3Config(reader *bufio.Reader, current config.S3Config) (config.S3Config, error) {
	endpoint, err := prompt(reader, "S3 endpoint", current.Endpoint)
	if err != nil {
		return config.S3Config{}, err
	}
	regionDefault := current.Region
	if strings.TrimSpace(regionDefault) == "" {
		regionDefault = "us-east-1"
	}
	region, err := prompt(reader, "S3 region", regionDefault)
	if err != nil {
		return config.S3Config{}, err
	}
	bucket, err := promptRequired(reader, "S3 bucket", current.Bucket)
	if err != nil {
		return config.S3Config{}, err
	}
	prefix, err := prompt(reader, "S3 prefix", current.Prefix)
	if err != nil {
		return config.S3Config{}, err
	}
	accessKeyID, err := prompt(reader, "S3 access key id", current.AccessKeyID)
	if err != nil {
		return config.S3Config{}, err
	}
	secretAccessKey, err := promptKeep(reader, "S3 secret access key", current.SecretAccessKey)
	if err != nil {
		return config.S3Config{}, err
	}
	return config.S3Config{
		Endpoint:        endpoint,
		Region:          region,
		Bucket:          bucket,
		Prefix:          prefix,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
	}, nil
}

func prompt(reader *bufio.Reader, label, current string) (string, error) {
	if current != "" {
		fmt.Fprintf(os.Stdout, "%s [%s]: ", label, current)
	} else {
		fmt.Fprintf(os.Stdout, "%s: ", label)
	}
	value, err := readLine(reader)
	if err != nil {
		return "", err
	}
	if value == "" {
		return current, nil
	}
	return value, nil
}

func promptRequired(reader *bufio.Reader, label, current string) (string, error) {
	for {
		value, err := prompt(reader, label, current)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" {
			return value, nil
		}
		fmt.Fprintf(os.Stdout, "%s is required.\n", label)
	}
}

func promptKeep(reader *bufio.Reader, label, current string) (string, error) {
	if current != "" {
		fmt.Fprintf(os.Stdout, "%s [leave blank to keep current]: ", label)
	} else {
		fmt.Fprintf(os.Stdout, "%s: ", label)
	}
	value, err := readLine(reader)
	if err != nil {
		return "", err
	}
	if value == "" {
		return current, nil
	}
	return value, nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) {
		if strings.TrimSpace(line) == "" {
			return "", err
		}
	}
	return strings.TrimSpace(line), nil
}
