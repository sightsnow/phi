package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"

	"phi/internal/model"
)

const (
	FormatVersion       = 1
	MasterKeySize       = 32
	DefaultArgonTime    = 1
	DefaultArgonMemory  = 64 * 1024
	DefaultArgonThreads = 4
)

var ErrInvalidCiphertext = errors.New("invalid ciphertext")

type KDFParams struct {
	Type    string `json:"type"`
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"key_len"`
}

func NewKDFParams() (KDFParams, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(cryptorand.Reader, salt); err != nil {
		return KDFParams{}, err
	}
	return KDFParams{
		Type:    "argon2id",
		Salt:    salt,
		Time:    DefaultArgonTime,
		Memory:  DefaultArgonMemory,
		Threads: DefaultArgonThreads,
		KeyLen:  MasterKeySize,
	}, nil
}

func EncodeKDFParams(params KDFParams) ([]byte, error) {
	return json.Marshal(params)
}

func DecodeKDFParams(data []byte) (KDFParams, error) {
	var params KDFParams
	if err := json.Unmarshal(data, &params); err != nil {
		return KDFParams{}, err
	}
	if params.Type != "argon2id" || len(params.Salt) == 0 || params.KeyLen == 0 {
		return KDFParams{}, errors.New("invalid kdf params")
	}
	return params, nil
}

func DeriveKey(passphrase []byte, params KDFParams) []byte {
	return argon2.IDKey(passphrase, params.Salt, params.Time, params.Memory, params.Threads, params.KeyLen)
}

func GenerateMasterKey() ([]byte, error) {
	masterKey := make([]byte, MasterKeySize)
	if _, err := io.ReadFull(cryptorand.Reader, masterKey); err != nil {
		return nil, err
	}
	return masterKey, nil
}

func WrapMasterKey(kek, masterKey []byte) ([]byte, error) {
	return seal(kek, masterKey)
}

func UnwrapMasterKey(kek, wrapped []byte) ([]byte, error) {
	return open(kek, wrapped)
}

func EncryptRecord(masterKey []byte, record model.KeyRecord) ([]byte, error) {
	plaintext, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	defer Zero(plaintext)
	return seal(masterKey, plaintext)
}

func DecryptRecord(masterKey, ciphertext []byte) (model.KeyRecord, error) {
	plaintext, err := open(masterKey, ciphertext)
	if err != nil {
		return model.KeyRecord{}, err
	}
	defer Zero(plaintext)

	var record model.KeyRecord
	if err := json.Unmarshal(plaintext, &record); err != nil {
		return model.KeyRecord{}, err
	}
	return record, nil
}

func Zero(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}

func seal(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(cryptorand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nonce, nonce, plaintext, nil)
	return sealed, nil
}

func open(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidCiphertext
	}
	nonce := ciphertext[:nonceSize]
	data := ciphertext[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	return plaintext, nil
}
