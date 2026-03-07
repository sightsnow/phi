package daemon

import (
	"context"
	"errors"
	"sync"
	"time"

	vaultagent "phi/internal/agent"
	"phi/internal/control"
	"phi/internal/crypto"
	"phi/internal/model"
	"phi/internal/platform"
	"phi/internal/session"
	storesqlite "phi/internal/store/sqlite"
)

type Service struct {
	session        *session.State
	controlNetwork string
	controlAddress string
	agentAddress   string
	vaultPath      string
	shutdown       context.CancelFunc
	pid            int

	agentMu     sync.Mutex
	agentServer *vaultagent.Server
}

type Options struct {
	PID            int
	ControlNetwork string
	ControlAddress string
}

func NewService(opts Options, shutdown context.CancelFunc) *Service {
	return &Service{
		session:        session.NewState(),
		controlNetwork: opts.ControlNetwork,
		controlAddress: opts.ControlAddress,
		agentAddress:   vaultagent.DefaultSocketPath(opts.ControlAddress),
		vaultPath:      platform.DefaultVaultPath(),
		shutdown:       shutdown,
		pid:            opts.PID,
	}
}

func (s *Service) Handle(ctx context.Context, req control.Request) control.Response {
	switch req.Action {
	case control.ActionStatus:
		return control.OK(s.status())
	case control.ActionGetAgentStatus:
		return control.OK(control.AgentStatusResponse{
			Enabled: s.agentRunning(),
			Address: s.agentAddress,
		})
	case control.ActionUnlock:
		return s.handleUnlock(ctx, req)
	case control.ActionLock:
		s.stopAgent()
		s.session.Lock()
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.shutdown()
		}()
		return control.OK(nil)
	case control.ActionChangePassphrase:
		return s.handleChangePassphrase(ctx, req)
	case control.ActionListKeys:
		return s.handleListKeys(ctx)
	case control.ActionGenerateKey:
		return s.handleGenerateKey(ctx, req)
	case control.ActionImportKey:
		return s.handleImportKey(ctx, req)
	case control.ActionRenameKey:
		return s.handleRenameKey(ctx, req)
	case control.ActionDeleteKey:
		return s.handleDeleteKey(ctx, req)
	case control.ActionSyncPush, control.ActionSyncPull:
		return control.Errorf("action %q is not implemented in daemon; use CLI sync command", req.Action)
	default:
		return control.Errorf("unknown action %q", req.Action)
	}
}

func (s *Service) status() model.DaemonStatus {
	return model.DaemonStatus{
		Running:        true,
		Unlocked:       s.session.IsUnlocked(),
		PID:            s.pid,
		ControlNetwork: s.controlNetwork,
		ControlAddress: s.controlAddress,
		AgentEnabled:   s.agentRunning(),
		AgentAddress:   s.agentAddress,
	}
}

func (s *Service) handleUnlock(ctx context.Context, req control.Request) control.Response {
	payload, err := control.Decode[control.UnlockRequest](req.Payload)
	if err != nil {
		return control.Errorf("decode unlock request: %v", err)
	}
	if len(payload.Passphrase) == 0 {
		return control.Errorf("empty passphrase")
	}
	vault, err := storesqlite.Open(s.vaultPath)
	if err != nil {
		return control.Errorf("open vault: %v", err)
	}
	defer vault.Close()

	masterKey, err := vault.Unlock(ctx, payload.Passphrase)
	if err != nil {
		return control.Errorf("unlock vault: %v", err)
	}
	defer crypto.Zero(masterKey)

	s.session.Unlock(masterKey)
	if err := s.startAgent(); err != nil {
		s.session.Lock()
		return control.Errorf("start agent: %v", err)
	}
	return control.OK(s.status())
}

func (s *Service) handleListKeys(ctx context.Context) control.Response {
	masterKey, err := s.masterKey()
	if err != nil {
		return control.Errorf("%s", err.Error())
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(s.vaultPath)
	if err != nil {
		return control.Errorf("open vault: %v", err)
	}
	defer vault.Close()

	keys, err := vault.ListKeys(ctx, masterKey)
	if err != nil {
		return control.Errorf("list keys: %v", err)
	}
	return control.OK(control.ListKeysResponse{Keys: keys})
}

func (s *Service) handleGenerateKey(ctx context.Context, req control.Request) control.Response {
	payload, err := control.Decode[control.GenerateKeyRequest](req.Payload)
	if err != nil {
		return control.Errorf("decode generate request: %v", err)
	}
	if payload.Name == "" {
		return control.Errorf("empty key name")
	}
	masterKey, err := s.masterKey()
	if err != nil {
		return control.Errorf("%s", err.Error())
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(s.vaultPath)
	if err != nil {
		return control.Errorf("open vault: %v", err)
	}
	defer vault.Close()

	summary, err := vault.GenerateKey(ctx, masterKey, payload.Name)
	if err != nil {
		return control.Errorf("generate key: %v", err)
	}
	return control.OK(summary)
}

func (s *Service) handleImportKey(ctx context.Context, req control.Request) control.Response {
	payload, err := control.Decode[control.ImportKeyRequest](req.Payload)
	if err != nil {
		return control.Errorf("decode import request: %v", err)
	}
	if payload.Path == "" {
		return control.Errorf("empty key path")
	}
	if payload.Name == "" {
		return control.Errorf("empty key name")
	}
	masterKey, err := s.masterKey()
	if err != nil {
		return control.Errorf("%s", err.Error())
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(s.vaultPath)
	if err != nil {
		return control.Errorf("open vault: %v", err)
	}
	defer vault.Close()

	summary, err := vault.ImportKey(ctx, masterKey, payload.Name, payload.Path)
	if err != nil {
		return control.Errorf("import key: %v", err)
	}
	return control.OK(summary)
}

func (s *Service) handleRenameKey(ctx context.Context, req control.Request) control.Response {
	payload, err := control.Decode[control.RenameKeyRequest](req.Payload)
	if err != nil {
		return control.Errorf("decode rename request: %v", err)
	}
	if payload.ID == "" {
		return control.Errorf("empty key id")
	}
	if payload.Name == "" {
		return control.Errorf("empty key name")
	}
	masterKey, err := s.masterKey()
	if err != nil {
		return control.Errorf("%s", err.Error())
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(s.vaultPath)
	if err != nil {
		return control.Errorf("open vault: %v", err)
	}
	defer vault.Close()

	summary, err := vault.RenameKey(ctx, masterKey, payload.ID, payload.Name)
	if err != nil {
		return control.Errorf("rename key: %v", err)
	}
	return control.OK(summary)
}

func (s *Service) handleDeleteKey(ctx context.Context, req control.Request) control.Response {
	payload, err := control.Decode[control.DeleteKeyRequest](req.Payload)
	if err != nil {
		return control.Errorf("decode delete request: %v", err)
	}
	if payload.ID == "" {
		return control.Errorf("empty key id")
	}
	if !s.session.IsUnlocked() {
		return control.Errorf("vault is locked; run phi unlock")
	}

	vault, err := storesqlite.Open(s.vaultPath)
	if err != nil {
		return control.Errorf("open vault: %v", err)
	}
	defer vault.Close()

	if err := vault.DeleteKey(ctx, payload.ID); err != nil {
		return control.Errorf("delete key: %v", err)
	}
	return control.OK(nil)
}

func (s *Service) handleChangePassphrase(ctx context.Context, req control.Request) control.Response {
	payload, err := control.Decode[control.ChangePassphraseRequest](req.Payload)
	if err != nil {
		return control.Errorf("decode change_passphrase request: %v", err)
	}
	defer crypto.Zero(payload.Passphrase)
	if len(payload.Passphrase) == 0 {
		return control.Errorf("empty passphrase")
	}

	masterKey, err := s.masterKey()
	if err != nil {
		return control.Errorf("%s", err.Error())
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(s.vaultPath)
	if err != nil {
		return control.Errorf("open vault: %v", err)
	}
	defer vault.Close()

	if err := vault.ChangePassphrase(ctx, masterKey, payload.Passphrase); err != nil {
		return control.Errorf("change passphrase: %v", err)
	}
	return control.OK(nil)
}

func (s *Service) masterKey() ([]byte, error) {
	masterKey, ok := s.session.MasterKeyCopy()
	if !ok {
		return nil, errors.New("vault is locked; run phi unlock")
	}
	return masterKey, nil
}

func (s *Service) startAgent() error {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.agentServer != nil {
		return nil
	}
	if s.agentAddress == "" {
		return errors.New("agent endpoint is empty")
	}
	server, err := vaultagent.Start(s.agentAddress, s.vaultPath, s.session)
	if err != nil {
		return err
	}
	s.agentServer = server
	return nil
}

func (s *Service) stopAgent() {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.agentServer == nil {
		return
	}
	_ = s.agentServer.Close()
	s.agentServer = nil
}

func (s *Service) agentRunning() bool {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	return s.agentServer != nil
}
