package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"phi/internal/config"
	"phi/internal/model"
	"phi/internal/store"
	storesqlite "phi/internal/store/sqlite"
)

var (
	ErrBackendNotConfigured = errors.New("sync backend is not configured")
	ErrConflict             = errors.New("sync conflict detected")
	ErrRemoteNotFound       = errors.New("remote vault not found")
)

type OnceAction string

const (
	OnceNoop OnceAction = "noop"
	OncePush OnceAction = "push"
	OncePull OnceAction = "pull"
)

type OnceResult struct {
	Action OnceAction
}

type StatusAction string

const (
	StatusNone     StatusAction = "none"
	StatusNoop     StatusAction = "noop"
	StatusPush     StatusAction = "push"
	StatusPull     StatusAction = "pull"
	StatusConflict StatusAction = "conflict"
)

type SnapshotStatus struct {
	Present bool
	Digest  string
	Meta    model.VaultMeta
}

type StatusResult struct {
	Backend string
	Local   SnapshotStatus
	Remote  SnapshotStatus
	Action  StatusAction
}

type backend interface {
	Download(context.Context) ([]byte, error)
	Upload(context.Context, []byte) error
}

type snapshot struct {
	Bytes    []byte
	Digest   string
	Revision int64
	Meta     model.VaultMeta
}

func TestConnection(ctx context.Context, cfg config.Config) error {
	client, err := newBackend(cfg)
	if err != nil {
		return err
	}
	_, err = client.Download(ctx)
	if err != nil && !errors.Is(err, ErrRemoteNotFound) {
		return err
	}
	return nil
}

func Status(ctx context.Context, cfg config.Config) (StatusResult, error) {
	client, err := newBackend(cfg)
	if err != nil {
		return StatusResult{}, err
	}

	result := StatusResult{Backend: backendName(cfg)}

	local, localErr := readLocalSnapshot(ctx, cfg.VaultPath)
	remote, remoteErr := readRemoteSnapshot(ctx, client)

	localMissing := isLocalSnapshotMissing(localErr)
	remoteMissing := errors.Is(remoteErr, ErrRemoteNotFound)

	if localErr != nil && !localMissing {
		return StatusResult{}, localErr
	}
	if remoteErr != nil && !remoteMissing {
		return StatusResult{}, remoteErr
	}

	if !localMissing {
		result.Local = snapshotStatusFromSnapshot(local)
	}
	if !remoteMissing {
		result.Remote = snapshotStatusFromSnapshot(remote)
	}
	result.Action = statusActionFor(result.Local, result.Remote)
	return result, nil
}

func Push(ctx context.Context, cfg config.Config) error {
	client, err := newBackend(cfg)
	if err != nil {
		return err
	}
	local, err := readLocalSnapshot(ctx, cfg.VaultPath)
	if err != nil {
		return err
	}
	remote, err := readRemoteSnapshot(ctx, client)
	if err != nil {
		if errors.Is(err, ErrRemoteNotFound) {
			return client.Upload(ctx, local.Bytes)
		}
		return err
	}
	if remote.Digest == local.Digest {
		return nil
	}
	if remote.Revision >= local.Revision {
		return fmt.Errorf("%w: remote revision=%d local revision=%d", ErrConflict, remote.Revision, local.Revision)
	}
	return client.Upload(ctx, local.Bytes)
}

func Once(ctx context.Context, cfg config.Config) (OnceResult, error) {
	client, err := newBackend(cfg)
	if err != nil {
		return OnceResult{}, err
	}

	local, localErr := readLocalSnapshot(ctx, cfg.VaultPath)
	remote, remoteErr := readRemoteSnapshot(ctx, client)

	localMissing := isLocalSnapshotMissing(localErr)
	remoteMissing := errors.Is(remoteErr, ErrRemoteNotFound)

	if localErr != nil && !localMissing {
		return OnceResult{}, localErr
	}
	if remoteErr != nil && !remoteMissing {
		return OnceResult{}, remoteErr
	}

	switch {
	case localMissing && remoteMissing:
		return OnceResult{}, errors.New("nothing to sync: local vault not found and remote vault not found")
	case localMissing:
		if err := writeLocalVault(cfg.VaultPath, remote.Bytes); err != nil {
			return OnceResult{}, err
		}
		return OnceResult{Action: OncePull}, nil
	case remoteMissing:
		if err := client.Upload(ctx, local.Bytes); err != nil {
			return OnceResult{}, err
		}
		return OnceResult{Action: OncePush}, nil
	case local.Digest == remote.Digest:
		return OnceResult{Action: OnceNoop}, nil
	case local.Revision > remote.Revision:
		if err := client.Upload(ctx, local.Bytes); err != nil {
			return OnceResult{}, err
		}
		return OnceResult{Action: OncePush}, nil
	case remote.Revision > local.Revision:
		if err := writeLocalVault(cfg.VaultPath, remote.Bytes); err != nil {
			return OnceResult{}, err
		}
		return OnceResult{Action: OncePull}, nil
	default:
		return OnceResult{}, fmt.Errorf("%w: local revision=%d remote revision=%d", ErrConflict, local.Revision, remote.Revision)
	}
}

func Pull(ctx context.Context, cfg config.Config) error {
	client, err := newBackend(cfg)
	if err != nil {
		return err
	}
	remote, err := readRemoteSnapshot(ctx, client)
	if err != nil {
		return err
	}
	local, err := readLocalSnapshot(ctx, cfg.VaultPath)
	if err != nil {
		if isLocalSnapshotMissing(err) {
			return writeLocalVault(cfg.VaultPath, remote.Bytes)
		}
		return err
	}
	if remote.Digest == local.Digest {
		return nil
	}
	if local.Revision >= remote.Revision {
		return fmt.Errorf("%w: local revision=%d remote revision=%d", ErrConflict, local.Revision, remote.Revision)
	}
	return writeLocalVault(cfg.VaultPath, remote.Bytes)
}

type s3Backend struct {
	client *s3.Client
	bucket string
	key    string
}

type webdavBackend struct {
	httpClient *http.Client
	fileURL    string
	username   string
	password   string
}

func newBackend(cfg config.Config) (backend, error) {
	switch backendName(cfg) {
	case "s3":
		return newS3Backend(cfg)
	case "webdav":
		return newWebDAVBackend(cfg)
	default:
		return nil, ErrBackendNotConfigured
	}
}

func backendName(cfg config.Config) string {
	backend := strings.ToLower(strings.TrimSpace(cfg.Sync.Backend))
	if backend != "" {
		return backend
	}
	if cfg.Sync.S3.Endpoint != "" || cfg.Sync.S3.Bucket != "" {
		return "s3"
	}
	if cfg.Sync.WebDAV.Endpoint != "" {
		return "webdav"
	}
	return ""
}

func newS3Backend(cfg config.Config) (backend, error) {
	s3cfg := cfg.Sync.S3
	if s3cfg.Bucket == "" {
		return nil, errors.New("sync.s3.bucket is required")
	}
	awsCfg := aws.Config{
		Region:      defaultString(s3cfg.Region, "us-east-1"),
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(s3cfg.AccessKeyID, s3cfg.SecretAccessKey, "")),
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = true
		if s3cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(s3cfg.Endpoint)
		}
	})
	return &s3Backend{
		client: client,
		bucket: s3cfg.Bucket,
		key:    remoteObjectName(cfg.VaultPath, s3cfg.Prefix),
	}, nil
}

func (b *s3Backend) Download(ctx context.Context) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b.bucket), Key: aws.String(b.key)})
	if err != nil {
		if isS3NotFound(err) {
			return nil, ErrRemoteNotFound
		}
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (b *s3Backend) Upload(ctx context.Context, data []byte) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.bucket),
		Key:           aws.String(b.key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String("application/octet-stream"),
	})
	return err
}

func isS3NotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}

func newWebDAVBackend(cfg config.Config) (backend, error) {
	webdav := cfg.Sync.WebDAV
	if webdav.Endpoint == "" {
		return nil, errors.New("sync.webdav.endpoint is required")
	}
	fileURL, err := buildWebDAVFileURL(webdav.Endpoint, webdav.Root, cfg.VaultPath)
	if err != nil {
		return nil, err
	}
	return &webdavBackend{
		httpClient: &http.Client{},
		fileURL:    fileURL,
		username:   webdav.Username,
		password:   webdav.Password,
	}, nil
}

func (b *webdavBackend) Download(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.fileURL, nil)
	if err != nil {
		return nil, err
	}
	b.authorize(req)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrRemoteNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("webdav get failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func (b *webdavBackend) Upload(ctx context.Context, data []byte) error {
	if err := b.ensureCollections(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, b.fileURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	b.authorize(req)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webdav put failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (b *webdavBackend) ensureCollections(ctx context.Context) error {
	u, err := url.Parse(b.fileURL)
	if err != nil {
		return err
	}
	parent := path.Dir(u.Path)
	if parent == "." || parent == "/" || parent == "" {
		return nil
	}
	parts := strings.Split(strings.Trim(parent, "/"), "/")
	current := ""
	for _, part := range parts {
		current = current + "/" + part
		collectionURL := *u
		collectionURL.Path = current
		req, err := http.NewRequestWithContext(ctx, "MKCOL", collectionURL.String(), nil)
		if err != nil {
			return err
		}
		b.authorize(req)
		resp, err := b.httpClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusCreated, http.StatusMethodNotAllowed, http.StatusNoContent, http.StatusMovedPermanently, http.StatusFound:
			continue
		default:
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				continue
			}
			return fmt.Errorf("webdav mkcol failed: %s", resp.Status)
		}
	}
	return nil
}

func (b *webdavBackend) authorize(req *http.Request) {
	if b.username != "" || b.password != "" {
		req.SetBasicAuth(b.username, b.password)
	}
}

func readLocalSnapshot(ctx context.Context, vaultPath string) (snapshot, error) {
	data, err := os.ReadFile(vaultPath)
	if err != nil {
		return snapshot{}, err
	}
	meta, err := inspectMeta(ctx, vaultPath)
	if err != nil {
		return snapshot{}, err
	}
	return snapshotFromData(data, meta), nil
}

func readRemoteSnapshot(ctx context.Context, client backend) (snapshot, error) {
	data, err := client.Download(ctx)
	if err != nil {
		return snapshot{}, err
	}
	meta, err := inspectMetaFromBytes(ctx, data)
	if err != nil {
		return snapshot{}, err
	}
	return snapshotFromData(data, meta), nil
}

func snapshotFromData(data []byte, meta model.VaultMeta) snapshot {
	digest := sha256.Sum256(data)
	return snapshot{
		Bytes:    data,
		Digest:   hex.EncodeToString(digest[:]),
		Revision: meta.Revision,
		Meta:     meta,
	}
}

func snapshotStatusFromSnapshot(value snapshot) SnapshotStatus {
	return SnapshotStatus{
		Present: true,
		Digest:  value.Digest,
		Meta:    value.Meta,
	}
}

func statusActionFor(local, remote SnapshotStatus) StatusAction {
	switch {
	case !local.Present && !remote.Present:
		return StatusNone
	case !local.Present:
		return StatusPull
	case !remote.Present:
		return StatusPush
	case local.Digest == remote.Digest:
		return StatusNoop
	case local.Meta.Revision > remote.Meta.Revision:
		return StatusPush
	case remote.Meta.Revision > local.Meta.Revision:
		return StatusPull
	default:
		return StatusConflict
	}
}

func inspectMeta(ctx context.Context, path string) (model.VaultMeta, error) {
	vault, err := storesqlite.Open(path)
	if err != nil {
		return model.VaultMeta{}, err
	}
	defer vault.Close()
	return vault.Meta(ctx)
}

func inspectMetaFromBytes(ctx context.Context, data []byte) (model.VaultMeta, error) {
	tempFile, err := os.CreateTemp("", "phi-sync-*.vault")
	if err != nil {
		return model.VaultMeta{}, err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return model.VaultMeta{}, err
	}
	if err := tempFile.Close(); err != nil {
		return model.VaultMeta{}, err
	}
	return inspectMeta(ctx, tempPath)
}

func isLocalSnapshotMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, store.ErrVaultNotInitialized)
}

func writeLocalVault(vaultPath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(vaultPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(vaultPath, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(vaultPath, 0o600)
}

func remoteObjectName(vaultPath, prefix string) string {
	base := filepath.Base(vaultPath)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return base
	}
	clean := strings.Trim(prefix, "/")
	if strings.HasSuffix(strings.ToLower(clean), ".phi") {
		return clean
	}
	return path.Join(clean, base)
}

func buildWebDAVFileURL(endpoint, root, vaultPath string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	remotePath := remoteObjectName(vaultPath, root)
	u.Path = joinURLPath(u.Path, remotePath)
	return u.String(), nil
}

func joinURLPath(basePath, extra string) string {
	if basePath == "" {
		basePath = "/"
	}
	joined := path.Join(basePath, extra)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	return joined
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
