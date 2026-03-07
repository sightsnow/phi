package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"phi/internal/config"
	"phi/internal/control"
	"phi/internal/model"
	"phi/internal/platform"
	storesqlite "phi/internal/store/sqlite"
	syncer "phi/internal/sync"
)

var (
	ErrInitPassphraseRequired = errors.New("vault passphrase is required to initialize the vault")
	ErrDaemonNotRunning       = errors.New("daemon is not running; run phi unlock")
	errDaemonNotReady         = errors.New("daemon is not ready")
)

type InitResult struct {
	ConfigCreated bool
	VaultCreated  bool
}

type CopyPublicKeyResult struct {
	Summary       model.KeySummary
	AlreadyExists bool
}

type Service struct {
	stdout io.Writer
	stderr io.Writer
}

func New(stdout, stderr io.Writer) *Service {
	return &Service{stdout: stdout, stderr: stderr}
}

func (s *Service) Init(ctx context.Context, passphrase []byte) (InitResult, error) {
	resolvedPath := platform.DefaultConfigPath()
	result := InitResult{}

	if platform.Exists(resolvedPath) {
		if _, err := config.Load(resolvedPath); err != nil {
			return InitResult{}, err
		}
	} else {
		if _, err := config.WriteDefault(resolvedPath); err != nil {
			return InitResult{}, err
		}
		result.ConfigCreated = true
	}
	vaultPath := platform.DefaultVaultPath()
	if platform.Exists(vaultPath) {
		return result, nil
	}
	if len(passphrase) == 0 {
		return result, ErrInitPassphraseRequired
	}
	if err := storesqlite.Create(ctx, vaultPath, passphrase); err != nil {
		return InitResult{}, err
	}
	result.VaultCreated = true
	return result, nil
}

func (s *Service) Status(ctx context.Context) (model.DaemonStatus, error) {
	client, err := s.clientFor()
	if err != nil {
		return model.DaemonStatus{}, err
	}
	var status model.DaemonStatus
	if err := client.Call(ctx, control.ActionStatus, nil, &status); err != nil {
		if isDialError(err) {
			return model.DaemonStatus{}, nil
		}
		return model.DaemonStatus{}, err
	}
	return status, nil
}

func (s *Service) Unlock(ctx context.Context, passphrase []byte) (model.DaemonStatus, error) {
	started, err := s.ensureDaemon(ctx)
	if err != nil {
		return model.DaemonStatus{}, err
	}
	client, err := s.clientFor()
	if err != nil {
		return model.DaemonStatus{}, err
	}
	var status model.DaemonStatus
	call := func() error {
		return client.Call(ctx, control.ActionUnlock, control.UnlockRequest{Passphrase: passphrase}, &status)
	}
	if started {
		err = retryDialError(ctx, 5*time.Second, call)
	} else {
		err = call()
	}
	if err != nil {
		if errors.Is(err, errDaemonNotReady) {
			return model.DaemonStatus{}, errors.New("daemon startup timeout")
		}
		return model.DaemonStatus{}, err
	}
	return status, nil
}

func (s *Service) Lock(ctx context.Context) error {
	client, err := s.clientFor()
	if err != nil {
		return err
	}
	if err := client.Call(ctx, control.ActionLock, nil, nil); err != nil {
		if isDialError(err) {
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) ChangePassphrase(ctx context.Context, passphrase []byte) error {
	client, err := s.clientFor()
	if err != nil {
		return err
	}
	if err := client.Call(ctx, control.ActionChangePassphrase, control.ChangePassphraseRequest{Passphrase: passphrase}, nil); err != nil {
		if isDialError(err) {
			return ErrDaemonNotRunning
		}
		return err
	}
	return nil
}

func (s *Service) AgentStatus(ctx context.Context) (control.AgentStatusResponse, error) {
	client, err := s.clientFor()
	if err != nil {
		return control.AgentStatusResponse{}, err
	}
	var status control.AgentStatusResponse
	if err := client.Call(ctx, control.ActionGetAgentStatus, nil, &status); err != nil {
		if isDialError(err) {
			return control.AgentStatusResponse{}, nil
		}
		return control.AgentStatusResponse{}, err
	}
	return status, nil
}

func (s *Service) ListKeys(ctx context.Context) ([]model.KeySummary, error) {
	client, err := s.clientFor()
	if err != nil {
		return nil, err
	}
	var response control.ListKeysResponse
	if err := client.Call(ctx, control.ActionListKeys, nil, &response); err != nil {
		if isDialError(err) {
			return nil, ErrDaemonNotRunning
		}
		return nil, err
	}
	return response.Keys, nil
}

func (s *Service) GenerateKey(ctx context.Context, name string) (model.KeySummary, error) {
	client, err := s.clientFor()
	if err != nil {
		return model.KeySummary{}, err
	}
	var summary model.KeySummary
	if err := client.Call(ctx, control.ActionGenerateKey, control.GenerateKeyRequest{Name: name}, &summary); err != nil {
		if isDialError(err) {
			return model.KeySummary{}, ErrDaemonNotRunning
		}
		return model.KeySummary{}, err
	}
	return summary, nil
}

func (s *Service) PublicKey(ctx context.Context, selector string) (model.KeySummary, error) {
	keys, err := s.ListKeys(ctx)
	if err != nil {
		return model.KeySummary{}, err
	}
	return matchKeySummary(keys, selector)
}

func (s *Service) CopyPublicKey(ctx context.Context, selector, target string, port int) (CopyPublicKeyResult, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return CopyPublicKeyResult{}, errors.New("empty remote target")
	}

	summary, err := s.PublicKey(ctx, selector)
	if err != nil {
		return CopyPublicKeyResult{}, err
	}
	alreadyExists, err := s.copyAuthorizedKey(ctx, target, port, summary.PublicKey)
	if err != nil {
		return CopyPublicKeyResult{}, err
	}
	return CopyPublicKeyResult{Summary: summary, AlreadyExists: alreadyExists}, nil
}

func (s *Service) RenameKey(ctx context.Context, selector, name string) (model.KeySummary, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.KeySummary{}, errors.New("empty key name")
	}

	summary, err := s.PublicKey(ctx, selector)
	if err != nil {
		return model.KeySummary{}, err
	}

	client, err := s.clientFor()
	if err != nil {
		return model.KeySummary{}, err
	}
	var renamed model.KeySummary
	if err := client.Call(ctx, control.ActionRenameKey, control.RenameKeyRequest{ID: summary.ID, Name: name}, &renamed); err != nil {
		if isDialError(err) {
			return model.KeySummary{}, ErrDaemonNotRunning
		}
		return model.KeySummary{}, err
	}
	return renamed, nil
}

func (s *Service) ImportKey(ctx context.Context, name, keyPath string) (model.KeySummary, error) {
	client, err := s.clientFor()
	if err != nil {
		return model.KeySummary{}, err
	}
	var summary model.KeySummary
	if err := client.Call(ctx, control.ActionImportKey, control.ImportKeyRequest{Name: name, Path: keyPath}, &summary); err != nil {
		if isDialError(err) {
			return model.KeySummary{}, ErrDaemonNotRunning
		}
		return model.KeySummary{}, err
	}
	return summary, nil
}

func (s *Service) DeleteKey(ctx context.Context, selector string) (model.KeySummary, error) {
	summary, err := s.PublicKey(ctx, selector)
	if err != nil {
		return model.KeySummary{}, err
	}

	client, err := s.clientFor()
	if err != nil {
		return model.KeySummary{}, err
	}
	if err := client.Call(ctx, control.ActionDeleteKey, control.DeleteKeyRequest{ID: summary.ID}, nil); err != nil {
		if isDialError(err) {
			return model.KeySummary{}, ErrDaemonNotRunning
		}
		return model.KeySummary{}, err
	}
	return summary, nil
}

func (s *Service) SyncStatus(ctx context.Context) (syncer.StatusResult, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return syncer.StatusResult{}, err
	}
	return syncer.Status(ctx, cfg)
}

func (s *Service) SyncPush(ctx context.Context) error {
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	return syncer.Push(ctx, cfg)
}

func (s *Service) SyncOnce(ctx context.Context) (syncer.OnceResult, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return syncer.OnceResult{}, err
	}
	return syncer.Once(ctx, cfg)
}

func (s *Service) SyncPull(ctx context.Context) error {
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	return syncer.Pull(ctx, cfg)
}

func (s *Service) loadConfig() (config.Config, error) {
	return config.Load(platform.DefaultConfigPath())
}

func (s *Service) clientFor() (*control.Client, error) {
	network, address, err := platform.ControlEndpoint(platform.DefaultControlPath())
	if err != nil {
		return nil, err
	}
	return control.NewClient(network, address), nil
}

func (s *Service) ensureDaemon(ctx context.Context) (bool, error) {
	client, err := s.clientFor()
	if err != nil {
		return false, err
	}
	var status model.DaemonStatus
	if err := client.Call(ctx, control.ActionStatus, nil, &status); err == nil && status.Running {
		return false, nil
	} else if err != nil && !isDialError(err) {
		return false, err
	}
	if err := s.startDaemon(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) startDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	network, address, err := platform.ControlEndpoint(platform.DefaultControlPath())
	if err != nil {
		return err
	}
	if network == "unix" {
		if err := platform.EnsureParentDir(address); err != nil {
			return err
		}
	}
	cmd := exec.Command(exe, "__daemon")
	cmd.Dir, _ = os.Getwd()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	prepareDaemonCommand(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

func retryDialError(ctx context.Context, timeout time.Duration, call func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := call()
		if err == nil {
			return nil
		}
		if !isDialError(err) {
			return err
		}
		if time.Now().After(deadline) {
			return errDaemonNotReady
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func isDialError(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var remoteErr *control.RemoteError
	if errors.As(err, &remoteErr) {
		return false
	}
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, net.ErrClosed)
}

func FormatStatus(status model.DaemonStatus) string {
	if !status.Running {
		return "daemon: stopped\nunlocked: no"
	}

	agent := "disabled"
	if status.AgentEnabled && status.AgentAddress != "" {
		agent = status.AgentAddress
	}

	return fmt.Sprintf(
		"daemon: running\nunlocked: %t\npid: %d\ncontrol: %s\nagent: %s",
		status.Unlocked,
		status.PID,
		status.ControlAddress,
		agent,
	)
}

func (s *Service) copyAuthorizedKey(ctx context.Context, target string, port int, publicKey string) (bool, error) {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return false, errors.New("empty public key")
	}
	script := fmt.Sprintf(
		"umask 077 && mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys && if grep -qxF %s ~/.ssh/authorized_keys; then exit 10; else printf '%%s\n' %s >> ~/.ssh/authorized_keys; fi",
		quotePOSIXShell(publicKey),
		quotePOSIXShell(publicKey),
	)
	args := []string{}
	if port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", port))
	}
	args = append(args, target, "sh -c "+quotePOSIXShell(script))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 10 {
			return true, nil
		}
		return false, fmt.Errorf("copy public key via ssh: %w", err)
	}
	return false, nil
}

func matchKeySummary(keys []model.KeySummary, selector string) (model.KeySummary, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return model.KeySummary{}, errors.New("empty key selector")
	}

	exact := filterKeySummaries(keys, func(key model.KeySummary) bool {
		return key.ID == selector || key.Name == selector
	})
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return model.KeySummary{}, fmt.Errorf("ambiguous key selector %q: %s", selector, formatKeyCandidates(exact))
	}

	prefix := filterKeySummaries(keys, func(key model.KeySummary) bool {
		return strings.HasPrefix(key.ID, selector)
	})
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return model.KeySummary{}, fmt.Errorf("ambiguous key selector %q: %s", selector, formatKeyCandidates(prefix))
	}

	return model.KeySummary{}, fmt.Errorf("key %q not found", selector)
}

func filterKeySummaries(keys []model.KeySummary, keep func(model.KeySummary) bool) []model.KeySummary {
	filtered := make([]model.KeySummary, 0, len(keys))
	for _, key := range keys {
		if keep(key) {
			filtered = append(filtered, key)
		}
	}
	return filtered
}

func formatKeyCandidates(keys []model.KeySummary) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s(%s)", key.Name, key.ID))
	}
	return strings.Join(parts, ", ")
}

func quotePOSIXShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
