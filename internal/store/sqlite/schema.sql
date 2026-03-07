PRAGMA foreign_keys = OFF;

CREATE TABLE IF NOT EXISTS meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  format_version INTEGER NOT NULL,
  kdf_params BLOB NOT NULL,
  wrapped_master_key BLOB NOT NULL,
  revision INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS keys (
  id TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  ciphertext BLOB NOT NULL
);
