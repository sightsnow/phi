package agent

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"

	"phi/internal/crypto"
	"phi/internal/model"
	storesqlite "phi/internal/store/sqlite"
)

var ErrMutationUnsupported = errors.New("agent mutation is not supported")

type MasterKeyProvider interface {
	MasterKeyCopy() ([]byte, bool)
	IsUnlocked() bool
}

type Server struct {
	endpoint      string
	listener      net.Listener
	removeOnClose bool
	closed        chan struct{}
	closeOnce     sync.Once
	impl          *VaultAgent
}

type VaultAgent struct {
	vaultPath string
	session   MasterKeyProvider
}

func DefaultSocketPath(controlPath string) string {
	if controlPath == "" {
		return ""
	}
	if strings.HasPrefix(controlPath, `\\.\pipe\`) {
		name := strings.TrimPrefix(controlPath, `\\.\pipe\`)
		name = strings.Trim(name, `\\`)
		if name == "" {
			return `\\.\pipe\phi-agent`
		}
		replaced := strings.Replace(name, "control", "agent", 1)
		if replaced == name {
			replaced = name + "-agent"
		}
		return `\\.\pipe\` + replaced
	}
	if strings.HasPrefix(controlPath, "npipe://") {
		name := strings.TrimPrefix(controlPath, "npipe://")
		name = strings.TrimPrefix(name, "./pipe/")
		name = strings.Trim(name, "/")
		if name == "" {
			return `\\.\pipe\phi-agent`
		}
		replaced := strings.ReplaceAll(name, "/", `\\`)
		replaced = strings.Replace(replaced, "control", "agent", 1)
		return `\\.\pipe\` + replaced
	}
	if strings.Contains(controlPath, "://") {
		return ""
	}
	if !strings.Contains(controlPath, string(filepath.Separator)) && strings.Contains(controlPath, ":") {
		return ""
	}
	base := filepath.Dir(controlPath)
	return filepath.Join(base, "agent.sock")
}

func Start(endpoint, vaultPath string, session MasterKeyProvider) (*Server, error) {
	if endpoint == "" {
		return nil, errors.New("empty agent endpoint")
	}
	listener, removeOnClose, err := listen(endpoint)
	if err != nil {
		return nil, err
	}
	server := &Server{
		endpoint:      endpoint,
		listener:      listener,
		removeOnClose: removeOnClose,
		closed:        make(chan struct{}),
		impl: &VaultAgent{
			vaultPath: vaultPath,
			session:   session,
		},
	}
	go server.serve()
	return server, nil
}

func (s *Server) Endpoint() string {
	if s == nil {
		return ""
	}
	return s.endpoint
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.closeOnce.Do(func() {
		err = s.listener.Close()
		<-s.closed
		if s.removeOnClose {
			_ = os.Remove(s.endpoint)
		}
	})
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func (s *Server) serve() {
	defer close(s.closed)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go func() {
			defer conn.Close()
			_ = sshagent.ServeAgent(s.impl, conn)
		}()
	}
}

func (a *VaultAgent) List() ([]*sshagent.Key, error) {
	masterKey, ok := a.session.MasterKeyCopy()
	if !ok {
		return []*sshagent.Key{}, nil
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(a.vaultPath)
	if err != nil {
		return nil, err
	}
	defer vault.Close()

	summaries, err := vault.ListKeys(context.Background(), masterKey)
	if err != nil {
		return nil, err
	}
	keys := make([]*sshagent.Key, 0, len(summaries))
	for _, summary := range summaries {
		publicKey, err := parseAuthorizedKey(summary.PublicKey)
		if err != nil {
			return nil, err
		}
		keys = append(keys, &sshagent.Key{
			Format:  publicKey.Type(),
			Blob:    publicKey.Marshal(),
			Comment: summary.Name,
		})
	}
	return keys, nil
}

func (a *VaultAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return a.SignWithFlags(key, data, 0)
}

func (a *VaultAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags sshagent.SignatureFlags) (*ssh.Signature, error) {
	masterKey, ok := a.session.MasterKeyCopy()
	if !ok {
		return nil, errors.New("agent is locked")
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(a.vaultPath)
	if err != nil {
		return nil, err
	}
	defer vault.Close()

	record, err := vault.LoadKey(context.Background(), masterKey, ssh.FingerprintSHA256(key))
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(record.PrivateKey)

	signer, err := signerFromRecord(record)
	if err != nil {
		return nil, err
	}
	if flags == 0 {
		return signer.Sign(cryptorand.Reader, data)
	}
	algorithmSigner, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("signature algorithm flags are not supported by %T", signer)
	}
	switch flags {
	case sshagent.SignatureFlagRsaSha256:
		return algorithmSigner.SignWithAlgorithm(cryptorand.Reader, data, ssh.KeyAlgoRSASHA256)
	case sshagent.SignatureFlagRsaSha512:
		return algorithmSigner.SignWithAlgorithm(cryptorand.Reader, data, ssh.KeyAlgoRSASHA512)
	default:
		return nil, fmt.Errorf("unsupported signature flags: %d", flags)
	}
}

func (a *VaultAgent) Add(sshagent.AddedKey) error {
	return ErrMutationUnsupported
}

func (a *VaultAgent) Remove(ssh.PublicKey) error {
	return ErrMutationUnsupported
}

func (a *VaultAgent) RemoveAll() error {
	return ErrMutationUnsupported
}

func (a *VaultAgent) Lock([]byte) error {
	return ErrMutationUnsupported
}

func (a *VaultAgent) Unlock([]byte) error {
	return ErrMutationUnsupported
}

func (a *VaultAgent) Signers() ([]ssh.Signer, error) {
	masterKey, ok := a.session.MasterKeyCopy()
	if !ok {
		return nil, errors.New("agent is locked")
	}
	defer crypto.Zero(masterKey)

	vault, err := storesqlite.Open(a.vaultPath)
	if err != nil {
		return nil, err
	}
	defer vault.Close()

	summaries, err := vault.ListKeys(context.Background(), masterKey)
	if err != nil {
		return nil, err
	}
	signers := make([]ssh.Signer, 0, len(summaries))
	for _, summary := range summaries {
		record, err := vault.LoadKey(context.Background(), masterKey, summary.ID)
		if err != nil {
			return nil, err
		}
		signer, err := signerFromRecord(record)
		crypto.Zero(record.PrivateKey)
		if err != nil {
			return nil, err
		}
		signers = append(signers, signer)
	}
	return signers, nil
}

func (a *VaultAgent) Extension(string, []byte) ([]byte, error) {
	return nil, sshagent.ErrExtensionUnsupported
}

func signerFromRecord(record model.KeyRecord) (ssh.Signer, error) {
	rawKey, err := ssh.ParseRawPrivateKey(record.PrivateKey)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(rawKey)
}

func parseAuthorizedKey(value string) (ssh.PublicKey, error) {
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(value)))
	if err == nil {
		return publicKey, nil
	}
	publicKey, err = ssh.ParsePublicKey([]byte(value))
	if err == nil {
		return publicKey, nil
	}
	return nil, err
}

func keyEqual(a, b ssh.PublicKey) bool {
	return bytes.Equal(a.Marshal(), b.Marshal())
}
