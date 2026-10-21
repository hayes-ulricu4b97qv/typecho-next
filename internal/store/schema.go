package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

const (
	currentSchemaVersion                        = 2
	sqliteBusyTimeoutMilliseconds               = 5000
	passwordResetCleanupBusyTimeoutMilliseconds = 100
)

func NewSQLiteRepository(path string) (*SQLiteRepository, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// Do not enable WAL until the database has passed schema validation. SQLite
	// persists journal_mode, so applying it while opening an unknown database
	// would modify a file that we are about to reject.
	dsn := fmt.Sprintf("file:%s?_busy_timeout=%d&_foreign_keys=on", filepath.ToSlash(path), sqliteBusyTimeoutMilliseconds)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	repo := &SQLiteRepository{db: db}
	if err := repo.createSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable database WAL mode: %w", err)
	}
	return repo, nil
}

func (r *SQLiteRepository) createSchema() error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("start schema creation: %w", err)
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	var existingTables int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT GLOB 'sqlite_*'`).Scan(&existingTables); err != nil {
		return fmt.Errorf("inspect database schema: %w", err)
	}
	if version == currentSchemaVersion {
		if err := validateCurrentSchema(tx); err != nil {
			return fmt.Errorf("validate database schema version %d: %w", version, err)
		}
		return tx.Commit()
	}
	if version != 0 {
		return fmt.Errorf("unsupported database schema version %d (expected schema version %d); the database was not modified", version, currentSchemaVersion)
	}
	legacy := existingTables != 0
	if legacy {
		recognized, err := isRecognizedLegacySchema(tx)
		if err != nil {
			return fmt.Errorf("inspect legacy database schema: %w", err)
		}
		if !recognized {
			return fmt.Errorf("unrecognized unversioned database; the database was not modified. Back it up, open it with its matching application version, or configure an empty APP_DATA_DIR")
		}
		if err := addLegacyColumns(tx); err != nil {
			return fmt.Errorf("upgrade legacy database columns: %w", err)
		}
		if err := normalizeLegacyTimestamps(tx); err != nil {
			return fmt.Errorf("normalize legacy database timestamps: %w", err)
		}
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS sites (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			admin_mail TEXT NOT NULL,
			timezone TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			keywords TEXT NOT NULL DEFAULT '',
			posts_per_page INTEGER NOT NULL DEFAULT 10 CHECK (posts_per_page BETWEEN 1 AND 50),
			comments_enabled INTEGER NOT NULL DEFAULT 1 CHECK (comments_enabled IN (0, 1)),
			comments_require_moderation INTEGER NOT NULL DEFAULT 1 CHECK (comments_require_moderation IN (0, 1)),
			comments_threaded INTEGER NOT NULL DEFAULT 1 CHECK (comments_threaded IN (0, 1)),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			email TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'editor' CHECK (role IN ('owner', 'editor', 'moderator')),
			active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS users_role_active_idx ON users(role, active)`,
		`CREATE TABLE IF NOT EXISTS posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE,
			excerpt TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
			allow_comment INTEGER NOT NULL DEFAULT 1 CHECK (allow_comment IN (0, 1)),
			published_at TEXT,
			updated_at TEXT NOT NULL,
			passphrase_hash TEXT NOT NULL DEFAULT '',
			custom_fields TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS posts_status_updated_idx ON posts(status, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS posts_status_published_idx ON posts(status, published_at DESC, id DESC)`,
		`CREATE TABLE IF NOT EXISTS pages (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE,
			excerpt TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
			published_at TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS pages_status_updated_idx ON pages(status, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS terms (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL COLLATE NOCASE,
			slug TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('category', 'tag')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(kind, name),
			UNIQUE(kind, slug)
		)`,
		`CREATE INDEX IF NOT EXISTS terms_kind_name_idx ON terms(kind, name)`,
		`CREATE TABLE IF NOT EXISTS post_terms (
			post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			term_id TEXT NOT NULL REFERENCES terms(id) ON DELETE CASCADE,
			PRIMARY KEY(post_id, term_id)
		)`,
		`CREATE INDEX IF NOT EXISTS post_terms_term_idx ON post_terms(term_id, post_id)`,
		`CREATE TABLE IF NOT EXISTS comments (
			id TEXT PRIMARY KEY,
			post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			parent_id TEXT NOT NULL DEFAULT '',
			author_name TEXT NOT NULL,
			author_email TEXT NOT NULL DEFAULT '',
			author_url TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'spam')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS comments_post_status_created_idx ON comments(post_id, status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS comments_parent_idx ON comments(parent_id, post_id)`,
		`CREATE TABLE IF NOT EXISTS media (
			id TEXT PRIMARY KEY,
			filename TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size INTEGER NOT NULL,
			storage_key TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS media_created_idx ON media(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS sessions_expires_idx ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id)`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			actor_user_id TEXT NOT NULL DEFAULT '',
			actor_username TEXT NOT NULL,
			action TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL DEFAULT '',
			details TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS audit_events_created_idx ON audit_events(created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS audit_events_resource_idx ON audit_events(resource_type, action, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS search_tokens (
			kind TEXT NOT NULL,
			token TEXT NOT NULL,
			record_id TEXT NOT NULL,
			PRIMARY KEY (kind, token, record_id)
		)`,
		`CREATE TABLE IF NOT EXISTS post_revisions (
			id TEXT PRIMARY KEY,
			post_id TEXT NOT NULL,
			title TEXT NOT NULL,
			excerpt TEXT NOT NULL,
			content TEXT NOT NULL,
			custom_fields TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS post_revisions_post_idx ON post_revisions(post_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS outbound_mail (
			id TEXT PRIMARY KEY,
			recipient TEXT NOT NULL,
			subject TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS outbound_mail_created_idx ON outbound_mail(created_at DESC, id DESC)`,
		`CREATE TABLE IF NOT EXISTS password_resets (
			token_hash TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("create database schema: %w", err)
		}
	}
	if err := validateCurrentSchema(tx); err != nil {
		return fmt.Errorf("validate new database schema: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, currentSchemaVersion)); err != nil {
		return fmt.Errorf("set database schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit database schema: %w", err)
	}
	return nil
}
