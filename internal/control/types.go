package control

import (
	"encoding/json"
	"fmt"

	"phi/internal/model"
)

const (
	ActionStatus           = "status"
	ActionUnlock           = "unlock"
	ActionLock             = "lock"
	ActionChangePassphrase = "change_passphrase"
	ActionListKeys         = "list_keys"
	ActionGenerateKey      = "generate_key"
	ActionImportKey        = "import_key"
	ActionRenameKey        = "rename_key"
	ActionDeleteKey        = "delete_key"
	ActionSyncPush         = "sync_push"
	ActionSyncPull         = "sync_pull"
	ActionGetAgentStatus   = "get_agent_status"
)

type Request struct {
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type UnlockRequest struct {
	Passphrase []byte `json:"passphrase"`
}

type ImportKeyRequest struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type GenerateKeyRequest struct {
	Name string `json:"name"`
}

type RenameKeyRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ChangePassphraseRequest struct {
	Passphrase []byte `json:"passphrase"`
}

type DeleteKeyRequest struct {
	ID string `json:"id"`
}

type ListKeysResponse struct {
	Keys []model.KeySummary `json:"keys"`
}

type AgentStatusResponse struct {
	Enabled bool   `json:"enabled"`
	Address string `json:"address"`
}

type RemoteError struct {
	Message string
}

func (e *RemoteError) Error() string {
	return e.Message
}

func NewRequest(action string, payload any) (Request, error) {
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return Request{}, err
		}
		raw = data
	}
	return Request{Action: action, Payload: raw}, nil
}

func OK(payload any) Response {
	if payload == nil {
		return Response{OK: true}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return Errorf("marshal response payload: %v", err)
	}
	return Response{OK: true, Payload: data}
}

func Errorf(format string, args ...any) Response {
	return Response{OK: false, Error: fmt.Sprintf(format, args...)}
}

func Decode[T any](raw json.RawMessage) (T, error) {
	var value T
	if len(raw) == 0 {
		return value, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}
