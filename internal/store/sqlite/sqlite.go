package sqlite

import (
	"context"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/x509"
	"database/sql"
	_ "embed"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"

	"phi/internal/crypto"
	"phi/internal/model"
	"phi/internal/platform"
	"phi/internal/store"
)

var (
	//go:embed schema.sql
	Schema string
)

type Store struct {
	path string
	db   *sql.DB
}

type metaRow struct {
	FormatVersion    int
	KDFParams        []byte
	WrappedMasterKey []byte
	Revision         int64
	CreatedAt        int64
	UpdatedAt        int64
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("empty vault path")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrVaultNotInitialized
		}
		return nil, err
	}
	return openDatabase(path)
}

func Create(ctx context.Context, path string, passphrase []byte) error {
	if len(passphrase) == 0 {
		return errors.New("empty passphrase")
	}
	if err := platform.EnsureParentDir(path); err != nil {
		return err
	}

	vault, err := openDatabase(path)
	if err != nil {
		return err
	}
	defer vault.Close()

	initialized, err := vault.IsInitialized(ctx)
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}

	params, err := crypto.NewKDFParams()
	if err != nil {
		return err
	}
	paramsBytes, err := crypto.EncodeKDFParams(params)
	if err != nil {
		return err
	}
	masterKey, err := crypto.GenerateMasterKey()
	if err != nil {
		return err
	}
	defer crypto.Zero(masterKey)

	kek := crypto.DeriveKey(passphrase, params)
	defer crypto.Zero(kek)

	wrappedMasterKey, err := crypto.WrapMasterKey(kek, masterKey)
	if err != nil {
		return err
	}
	defer crypto.Zero(wrappedMasterKey)

	now := time.Now().Unix()
	tx, err := vault.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO meta (id, format_version, kdf_params, wrapped_master_key, revision, created_at, updated_at)
		 VALUES (1, ?, ?, ?, 0, ?, ?)`,
		crypto.FormatVersion,
		paramsBytes,
		wrappedMasterKey,
		now,
		now,
	); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) IsInitialized(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM meta WHERE id = 1`).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

func (s *Store) Meta(ctx context.Context) (model.VaultMeta, error) {
	meta, err := s.loadMeta(ctx)
	if err != nil {
		return model.VaultMeta{}, err
	}
	return model.VaultMeta{
		FormatVersion: meta.FormatVersion,
		Revision:      meta.Revision,
		CreatedAt:     time.Unix(meta.CreatedAt, 0),
		UpdatedAt:     time.Unix(meta.UpdatedAt, 0),
	}, nil
}

func (s *Store) Unlock(ctx context.Context, passphrase []byte) ([]byte, error) {
	meta, err := s.loadMeta(ctx)
	if err != nil {
		return nil, err
	}
	params, err := crypto.DecodeKDFParams(meta.KDFParams)
	if err != nil {
		return nil, err
	}
	kek := crypto.DeriveKey(passphrase, params)
	defer crypto.Zero(kek)

	masterKey, err := crypto.UnwrapMasterKey(kek, meta.WrappedMasterKey)
	if err != nil {
		return nil, store.ErrWrongPassphrase
	}
	return masterKey, nil
}

func (s *Store) ChangePassphrase(ctx context.Context, masterKey, passphrase []byte) error {
	if len(masterKey) == 0 {
		return errors.New("empty master key")
	}
	if len(passphrase) == 0 {
		return errors.New("empty passphrase")
	}

	params, err := crypto.NewKDFParams()
	if err != nil {
		return err
	}
	paramsBytes, err := crypto.EncodeKDFParams(params)
	if err != nil {
		return err
	}
	kek := crypto.DeriveKey(passphrase, params)
	defer crypto.Zero(kek)

	wrappedMasterKey, err := crypto.WrapMasterKey(kek, masterKey)
	if err != nil {
		return err
	}
	defer crypto.Zero(wrappedMasterKey)

	now := time.Now().Unix()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE meta SET kdf_params = ?, wrapped_master_key = ?, updated_at = ? WHERE id = 1`,
		paramsBytes,
		wrappedMasterKey,
		now,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return store.ErrVaultNotInitialized
	}
	return nil
}

func (s *Store) ListKeys(ctx context.Context, masterKey []byte) ([]model.KeySummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, created_at, updated_at, ciphertext FROM keys ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []model.KeySummary
	for rows.Next() {
		var (
			id         string
			createdAt  int64
			updatedAt  int64
			ciphertext []byte
		)
		if err := rows.Scan(&id, &createdAt, &updatedAt, &ciphertext); err != nil {
			return nil, err
		}
		record, err := crypto.DecryptRecord(masterKey, ciphertext)
		if err != nil {
			return nil, err
		}
		keys = append(keys, model.KeySummary{
			ID:        id,
			Name:      record.Name,
			Algorithm: record.Algorithm,
			PublicKey: record.PublicKey,
			CreatedAt: time.Unix(createdAt, 0),
			UpdatedAt: time.Unix(updatedAt, 0),
		})
		crypto.Zero(record.PrivateKey)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *Store) LoadKey(ctx context.Context, masterKey []byte, keyID string) (model.KeyRecord, error) {
	var ciphertext []byte
	if err := s.db.QueryRowContext(ctx, `SELECT ciphertext FROM keys WHERE id = ?`, keyID).Scan(&ciphertext); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.KeyRecord{}, store.ErrKeyNotFound
		}
		return model.KeyRecord{}, err
	}
	return crypto.DecryptRecord(masterKey, ciphertext)
}

func (s *Store) GenerateKey(ctx context.Context, masterKey []byte, name string) (model.KeySummary, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.KeySummary{}, errors.New("empty key name")
	}

	_, privateKey, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer crypto.Zero(privateKey)

	privateKeyBytes, err := marshalPrivateKeyPEM(privateKey)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer crypto.Zero(privateKeyBytes)

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return model.KeySummary{}, err
	}
	publicKey := signer.PublicKey()

	record := model.KeyRecord{
		Name:       name,
		Algorithm:  publicKey.Type(),
		PublicKey:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey))),
		PrivateKey: append([]byte(nil), privateKeyBytes...),
	}
	defer crypto.Zero(record.PrivateKey)

	return s.insertKeyRecord(ctx, masterKey, record)
}

func (s *Store) ImportKey(ctx context.Context, masterKey []byte, name, privateKeyPath string) (model.KeySummary, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.KeySummary{}, errors.New("empty key name")
	}

	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer crypto.Zero(privateKeyBytes)

	rawKey, err := ssh.ParseRawPrivateKey(privateKeyBytes)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return model.KeySummary{}, errors.New("encrypted private key import is not supported yet")
		}
		return model.KeySummary{}, err
	}

	signer, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		return model.KeySummary{}, err
	}
	publicKey := signer.PublicKey()
	publicKeyText := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	privateKeyCopy := append([]byte(nil), privateKeyBytes...)
	defer crypto.Zero(privateKeyCopy)

	record := model.KeyRecord{
		Name:       name,
		Algorithm:  publicKey.Type(),
		PublicKey:  publicKeyText,
		PrivateKey: privateKeyCopy,
	}

	return s.insertKeyRecord(ctx, masterKey, record)
}

func (s *Store) insertKeyRecord(ctx context.Context, masterKey []byte, record model.KeyRecord) (model.KeySummary, error) {
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(record.PublicKey))
	if err != nil {
		return model.KeySummary{}, err
	}
	keyID := ssh.FingerprintSHA256(publicKey)

	ciphertext, err := crypto.EncryptRecord(masterKey, record)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer crypto.Zero(ciphertext)

	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO keys (id, created_at, updated_at, ciphertext) VALUES (?, ?, ?, ?)`,
		keyID,
		now,
		now,
		ciphertext,
	); err != nil {
		if isConstraintError(err) {
			return model.KeySummary{}, store.ErrDuplicateKey
		}
		return model.KeySummary{}, err
	}
	if err := bumpRevision(ctx, tx, now); err != nil {
		return model.KeySummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.KeySummary{}, err
	}

	return model.KeySummary{
		ID:        keyID,
		Name:      record.Name,
		Algorithm: record.Algorithm,
		PublicKey: record.PublicKey,
		CreatedAt: time.Unix(now, 0),
		UpdatedAt: time.Unix(now, 0),
	}, nil
}

func marshalPrivateKeyPEM(privateKey ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(der)
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return pem.EncodeToMemory(block), nil
}

func (s *Store) RenameKey(ctx context.Context, masterKey []byte, keyID, name string) (model.KeySummary, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.KeySummary{}, errors.New("empty key name")
	}

	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer tx.Rollback()

	var (
		createdAt  int64
		updatedAt  int64
		ciphertext []byte
	)
	if err := tx.QueryRowContext(ctx, `SELECT created_at, updated_at, ciphertext FROM keys WHERE id = ?`, keyID).Scan(&createdAt, &updatedAt, &ciphertext); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.KeySummary{}, store.ErrKeyNotFound
		}
		return model.KeySummary{}, err
	}

	record, err := crypto.DecryptRecord(masterKey, ciphertext)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer crypto.Zero(record.PrivateKey)

	if record.Name == name {
		return model.KeySummary{
			ID:        keyID,
			Name:      record.Name,
			Algorithm: record.Algorithm,
			PublicKey: record.PublicKey,
			CreatedAt: time.Unix(createdAt, 0),
			UpdatedAt: time.Unix(updatedAt, 0),
		}, nil
	}

	record.Name = name
	updatedCiphertext, err := crypto.EncryptRecord(masterKey, record)
	if err != nil {
		return model.KeySummary{}, err
	}
	defer crypto.Zero(updatedCiphertext)

	result, err := tx.ExecContext(ctx, `UPDATE keys SET updated_at = ?, ciphertext = ? WHERE id = ?`, now, updatedCiphertext, keyID)
	if err != nil {
		return model.KeySummary{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return model.KeySummary{}, err
	}
	if rowsAffected != 1 {
		return model.KeySummary{}, store.ErrKeyNotFound
	}
	if err := bumpRevision(ctx, tx, now); err != nil {
		return model.KeySummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.KeySummary{}, err
	}

	return model.KeySummary{
		ID:        keyID,
		Name:      record.Name,
		Algorithm: record.Algorithm,
		PublicKey: record.PublicKey,
		CreatedAt: time.Unix(createdAt, 0),
		UpdatedAt: time.Unix(now, 0),
	}, nil
}

func (s *Store) DeleteKey(ctx context.Context, keyID string) error {
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `DELETE FROM keys WHERE id = ?`, keyID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return store.ErrKeyNotFound
	}
	if err := bumpRevision(ctx, tx, now); err != nil {
		return err
	}
	return tx.Commit()
}

func openDatabase(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := configureDatabase(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(Schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{path: path, db: db}, nil
}

func configureDatabase(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}

	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode = DELETE`).Scan(&journalMode); err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(journalMode)) != "delete" {
		return fmt.Errorf("unexpected sqlite journal mode: %s", journalMode)
	}

	if _, err := db.Exec(`PRAGMA synchronous = FULL`); err != nil {
		return err
	}
	return nil
}

func (s *Store) loadMeta(ctx context.Context) (metaRow, error) {
	var meta metaRow
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT format_version, kdf_params, wrapped_master_key, revision, created_at, updated_at FROM meta WHERE id = 1`,
	).Scan(
		&meta.FormatVersion,
		&meta.KDFParams,
		&meta.WrappedMasterKey,
		&meta.Revision,
		&meta.CreatedAt,
		&meta.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return metaRow{}, store.ErrVaultNotInitialized
		}
		return metaRow{}, err
	}
	return meta, nil
}

func bumpRevision(ctx context.Context, tx *sql.Tx, now int64) error {
	result, err := tx.ExecContext(ctx, `UPDATE meta SET revision = revision + 1, updated_at = ? WHERE id = 1`, now)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return store.ErrVaultNotInitialized
	}
	return nil
}

func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint") || strings.Contains(message, "unique")
}

func (s *Store) String() string {
	return fmt.Sprintf("sqlite:%s", s.path)
}
