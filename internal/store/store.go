package store

import "errors"

var (
	ErrNotImplemented      = errors.New("feature is not implemented in this milestone")
	ErrVaultNotInitialized = errors.New("vault is not initialized; run phi init")
	ErrWrongPassphrase     = errors.New("wrong vault passphrase")
	ErrDuplicateKey        = errors.New("key already exists")
	ErrKeyNotFound         = errors.New("key not found")
)
