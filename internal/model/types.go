package model

import "time"

type KeyRecord struct {
	Name       string `json:"name"`
	Algorithm  string `json:"algorithm"`
	PublicKey  string `json:"public_key"`
	PrivateKey []byte `json:"private_key"`
}

type KeySummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Algorithm string    `json:"algorithm"`
	PublicKey string    `json:"public_key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type VaultMeta struct {
	FormatVersion int       `json:"format_version"`
	Revision      int64     `json:"revision"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type DaemonStatus struct {
	Running        bool   `json:"running"`
	Unlocked       bool   `json:"unlocked"`
	PID            int    `json:"pid"`
	ControlNetwork string `json:"control_network"`
	ControlAddress string `json:"control_address"`
	AgentEnabled   bool   `json:"agent_enabled"`
	AgentAddress   string `json:"agent_address"`
}
