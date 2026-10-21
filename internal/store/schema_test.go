package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshDatabaseUsesCompleteVersionedSchema(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	var version int
	if err := repo.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
	var publicationIndex int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'posts_status_published_idx'`).Scan(&publicationIndex); err != nil {
		t.Fatal(err)
	}
	if publicationIndex != 1 {
		t.Fatal("fresh schema lacks posts_status_published_idx")
	}

	for table, required := range map[string][]string{
		"sites":          {"description", "keywords", "posts_per_page", "comments_enabled", "comments_require_moderation", "comments_threaded"},
		"posts":          {"allow_comment", "passphrase_hash", "custom_fields"},
		"comments":       {"parent_id", "author_url"},
		"sessions":       {"user_id"},
		"post_revisions": {"custom_fields"},
	} {
		columns := tableColumns(t, repo, table)
		for _, column := range required {
			if !columns[column] {
				t.Errorf("fresh %s schema lacks %s", table, column)
			}
		}
	}
}

func TestOldDatabaseIsRejectedWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sites (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sites(id, name) VALUES (1, 'old site')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened an unversioned database")
	} else if !strings.Contains(err.Error(), "unrecognized unversioned database") {
		t.Fatalf("old database error = %v", err)
	}

	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err := db.QueryRow(`SELECT name FROM sites WHERE id = 1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "old site" {
		t.Fatalf("old database was modified: site name = %q", name)
	}
	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT GLOB 'sqlite_*'`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 1 {
		t.Fatalf("old database gained schema objects: %d tables", tableCount)
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "delete" {
		t.Fatalf("rejected database journal mode = %q, want delete", journalMode)
	}
}

func TestLegacyGoSchemaIsRejectedWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	createLegacyDatabase(t, path, "2025-03-04T05:06:07.890123456+08:00")

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened an unversioned legacy Go database")
	} else if !strings.Contains(err.Error(), "unrecognized unversioned database") {
		t.Fatalf("legacy Go database error = %v", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, customFieldsColumns int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('posts') WHERE name = 'custom_fields'").Scan(&customFieldsColumns); err != nil {
		t.Fatal(err)
	}
	var content string
	if err := db.QueryRow("SELECT content FROM posts WHERE id = 'legacy-post'").Scan(&content); err != nil {
		t.Fatal(err)
	}
	if version != 0 || customFieldsColumns != 0 || content != "Keep this body" {
		t.Fatalf("rejected legacy database changed: version=%d customFieldsColumns=%d content=%q", version, customFieldsColumns, content)
	}
}

func TestLegacyGoSchemaIsRejectedBeforeReadingTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	createLegacyDatabase(t, path, "not-a-timestamp")

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened an unversioned legacy database with an invalid timestamp")
	} else if !strings.Contains(err.Error(), "unrecognized unversioned database") {
		t.Fatalf("legacy database error = %v", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	var customFieldsColumns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('posts') WHERE name = 'custom_fields'`).Scan(&customFieldsColumns); err != nil {
		t.Fatal(err)
	}
	var storedTimestamp string
	if err := db.QueryRow(`SELECT updated_at FROM posts WHERE id = 'legacy-post'`).Scan(&storedTimestamp); err != nil {
		t.Fatal(err)
	}
	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT GLOB 'sqlite_*'`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if version != 0 || customFieldsColumns != 0 || storedTimestamp != "not-a-timestamp" || tableCount != 15 {
		t.Fatalf("rejected legacy database changed: version=%d customFieldsColumns=%d updated_at=%q tableCount=%d", version, customFieldsColumns, storedTimestamp, tableCount)
	}
}

func TestLegacyGoSchemaWithIncompleteHistoryIsRejectedWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	createLegacyDatabase(t, path, "2025-03-04T05:06:07.890123456+08:00")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = 8`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened a legacy database with incomplete migration history")
	} else if !strings.Contains(err.Error(), "unrecognized unversioned database") {
		t.Fatalf("incomplete legacy database error = %v", err)
	}

	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, customFieldsColumns int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('posts') WHERE name = 'custom_fields'`).Scan(&customFieldsColumns); err != nil {
		t.Fatal(err)
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if version != 0 || customFieldsColumns != 0 || journalMode != "delete" {
		t.Fatalf("rejected legacy database changed: version=%d customFieldsColumns=%d journalMode=%q", version, customFieldsColumns, journalMode)
	}
}

func TestLegacySchemaWithUnexpectedTriggerIsRejectedWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	createLegacyDatabase(t, path, "2025-03-04T05:06:07.890123456+08:00")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER unapproved_schema_trigger BEFORE UPDATE OF updated_at ON posts BEGIN SELECT RAISE(ABORT, 'unexpected trigger must not run'); END`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened an unversioned legacy database with an unexpected trigger")
	} else if !strings.Contains(err.Error(), `unrecognized unversioned database`) {
		t.Fatalf("unexpected legacy schema object error = %v", err)
	}

	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, customFieldsColumns, triggers int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('posts') WHERE name = 'custom_fields'`).Scan(&customFieldsColumns); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'unapproved_schema_trigger'`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if version != 0 || customFieldsColumns != 0 || triggers != 1 || journalMode != "delete" {
		t.Fatalf("rejected legacy database changed: version=%d customFieldsColumns=%d triggers=%d journalMode=%q", version, customFieldsColumns, triggers, journalMode)
	}
}

func TestCurrentVersionWithIncompleteSchemaIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	repo, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`DROP TABLE comments`); err != nil {
		_ = repo.Close()
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened a versioned database with an incomplete schema")
	} else if !strings.Contains(err.Error(), `required table "comments" is missing`) {
		t.Fatalf("incomplete current schema error = %v", err)
	}
}

func TestCurrentVersionWithUnexpectedSchemaObjectsIsRejected(t *testing.T) {
	tests := []struct {
		name        string
		statement   string
		wantInError string
	}{
		{
			name:        "extra index",
			statement:   `CREATE INDEX unexpected_posts_status_idx ON posts(status)`,
			wantInError: `unexpected index "unexpected_posts_status_idx"`,
		},
		{
			name:        "extra view",
			statement:   `CREATE VIEW unexpected_public_posts AS SELECT id FROM posts WHERE status = 'published'`,
			wantInError: `unexpected view "unexpected_public_posts"`,
		},
		{
			name:        "extra trigger",
			statement:   `CREATE TRIGGER unexpected_post_trigger AFTER INSERT ON posts BEGIN DELETE FROM posts WHERE id = NEW.id; END`,
			wantInError: `unexpected trigger "unexpected_post_trigger"`,
		},
		{
			name:        "generated column",
			statement:   `ALTER TABLE posts ADD COLUMN unexpected_generated TEXT GENERATED ALWAYS AS (title) VIRTUAL`,
			wantInError: `unexpected column posts.unexpected_generated`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.db")
			repo, err := NewSQLiteRepository(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.db.Exec(test.statement); err != nil {
				_ = repo.Close()
				t.Fatal(err)
			}
			if err := repo.Close(); err != nil {
				t.Fatal(err)
			}

			if repo, err := NewSQLiteRepository(path); err == nil {
				_ = repo.Close()
				t.Fatal("opened a versioned database with an unexpected schema object")
			} else if !strings.Contains(err.Error(), test.wantInError) {
				t.Fatalf("unexpected schema object error = %v, want it to contain %q", err, test.wantInError)
			}
		})
	}
}

func TestCurrentVersionWithMissingSchemaIndexIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	repo, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`DROP INDEX posts_status_published_idx`); err != nil {
		_ = repo.Close()
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened a versioned database with a missing schema index")
	} else if !strings.Contains(err.Error(), `required index "posts_status_published_idx" is missing`) {
		t.Fatalf("missing current index error = %v", err)
	}
}

func TestCurrentVersionWithDriftedSchemaIndexIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	repo, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`DROP INDEX posts_status_published_idx`); err != nil {
		_ = repo.Close()
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`CREATE INDEX posts_status_published_idx ON posts(status, published_at DESC, id ASC)`); err != nil {
		_ = repo.Close()
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened a versioned database with a noncanonical schema index")
	} else if !strings.Contains(err.Error(), `index "posts_status_published_idx" column 2`) {
		t.Fatalf("drifted current index error = %v", err)
	}
}

func TestSchemaColumnValidationChecksTypeAndPrimaryKey(t *testing.T) {
	tests := []struct {
		name          string
		definition    string
		wantErrorText string
	}{
		{
			name:          "wrong type",
			definition:    `CREATE TABLE posts (id INTEGER PRIMARY KEY)`,
			wantErrorText: `column posts.id has type "INTEGER", expected "TEXT"`,
		},
		{
			name:          "missing primary key",
			definition:    `CREATE TABLE posts (id TEXT)`,
			wantErrorText: `column posts.id has primary-key position 0, expected 1`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(test.definition); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			mismatch, err := schemaColumnShapeMismatch(tx, "posts", map[string]bool{"id": true})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(mismatch, test.wantErrorText) {
				t.Fatalf("column mismatch = %q, want it to contain %q", mismatch, test.wantErrorText)
			}
		})
	}
}

func TestSchemaColumnValidationChecksTableOptions(t *testing.T) {
	tests := []struct {
		name         string
		definition   string
		wantMismatch string
	}{
		{
			name:         "rowid table",
			definition:   `CREATE TABLE posts (id TEXT PRIMARY KEY)`,
			wantMismatch: "",
		},
		{
			name:         "without rowid",
			definition:   `CREATE TABLE posts (id TEXT PRIMARY KEY) WITHOUT ROWID`,
			wantMismatch: `without_rowid=true strict=false`,
		},
		{
			name:         "strict table",
			definition:   `CREATE TABLE posts (id TEXT PRIMARY KEY) STRICT`,
			wantMismatch: `without_rowid=false strict=true`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(test.definition); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			mismatch, err := schemaColumnShapeMismatch(tx, "posts", map[string]bool{"id": true})
			if err != nil {
				t.Fatal(err)
			}
			if test.wantMismatch == "" {
				if mismatch != "" {
					t.Fatalf("ordinary table mismatch = %q", mismatch)
				}
			} else if !strings.Contains(mismatch, test.wantMismatch) {
				t.Fatalf("table options mismatch = %q, want it to contain %q", mismatch, test.wantMismatch)
			}
		})
	}
}

func TestSchemaColumnValidationChecksCollation(t *testing.T) {
	tests := []struct {
		name         string
		definition   string
		table        string
		allowed      map[string]bool
		wantMismatch string
	}{
		{
			name:       "ordinary binary column",
			definition: `CREATE TABLE posts (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'draft')`,
			table:      "posts",
			allowed:    map[string]bool{"id": true, "status": true},
		},
		{
			name:         "nonunique column with nocase collation",
			definition:   `CREATE TABLE posts (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'draft' COLLATE NOCASE)`,
			table:        "posts",
			allowed:      map[string]bool{"id": true, "status": true},
			wantMismatch: `column posts.status has collation "NOCASE", expected "BINARY"`,
		},
		{
			name:       "terms name column nocase",
			definition: `CREATE TABLE terms (id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE, slug TEXT NOT NULL, kind TEXT NOT NULL, UNIQUE(kind, name), UNIQUE(kind, slug))`,
			table:      "terms",
			allowed:    map[string]bool{"id": true, "name": true, "slug": true, "kind": true},
		},
		{
			name:         "unique index nocase does not replace column collation",
			definition:   `CREATE TABLE terms (id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE BINARY, slug TEXT NOT NULL, kind TEXT NOT NULL, UNIQUE(kind, name COLLATE NOCASE), UNIQUE(kind, slug))`,
			table:        "terms",
			allowed:      map[string]bool{"id": true, "name": true, "slug": true, "kind": true},
			wantMismatch: `column terms.name has collation "BINARY", expected "NOCASE"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(test.definition); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			mismatch, err := schemaColumnShapeMismatch(tx, test.table, test.allowed)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantMismatch == "" {
				if mismatch != "" {
					t.Fatalf("column collation mismatch = %q", mismatch)
				}
			} else if !strings.Contains(mismatch, test.wantMismatch) {
				t.Fatalf("column collation mismatch = %q, want it to contain %q", mismatch, test.wantMismatch)
			}
		})
	}
}

func TestSchemaColumnValidationChecksDefault(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'viewer')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	allowed := map[string]bool{"id": true, "username": true, "role": true}
	mismatch, err := schemaColumnShapeMismatch(tx, "users", allowed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mismatch, `column users.role has default "'viewer'", expected "'editor'"`) {
		t.Fatalf("default mismatch = %q", mismatch)
	}
}

func TestSchemaColumnValidationReportsMismatchInStableOrder(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT,
		slug TEXT NOT NULL UNIQUE,
		excerpt TEXT DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'draft',
		allow_comment INTEGER NOT NULL DEFAULT 1,
		published_at TEXT,
		updated_at TEXT NOT NULL,
		passphrase_hash TEXT NOT NULL DEFAULT '',
		custom_fields TEXT NOT NULL DEFAULT '{}'
	)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	allowed := make(map[string]bool, len(currentSchemaTables[2].columns))
	for _, column := range currentSchemaTables[2].columns {
		allowed[column] = true
	}
	mismatch, err := schemaColumnShapeMismatch(tx, "posts", allowed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mismatch, "column posts.excerpt must be NOT NULL") {
		t.Fatalf("first reported mismatch = %q, want alphabetically first invalid column excerpt", mismatch)
	}
}

func TestUniqueConstraintValidationChecksPresenceAndCollation(t *testing.T) {
	tests := []struct {
		name       string
		definition string
		table      string
	}{
		{
			name:       "missing unique key",
			definition: `CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT NOT NULL)`,
			table:      "users",
		},
		{
			name:       "wrong unique collation",
			definition: `CREATE TABLE terms (id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE BINARY, slug TEXT NOT NULL, kind TEXT NOT NULL, UNIQUE(kind, name), UNIQUE(kind, slug))`,
			table:      "terms",
		},
		{
			name:       "wrong primary key collation",
			definition: `CREATE TABLE users (id TEXT COLLATE NOCASE PRIMARY KEY, username TEXT NOT NULL UNIQUE)`,
			table:      "users",
		},
		{
			name:       "wrong primary key order",
			definition: `CREATE TABLE users (id TEXT PRIMARY KEY DESC, username TEXT NOT NULL UNIQUE)`,
			table:      "users",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(test.definition); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := validateUniqueConstraints(tx, test.table); err == nil {
				t.Fatal("accepted a drifted unique constraint")
			}
		})
	}
}

func TestForeignKeyValidationChecksDeleteAction(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY); CREATE TABLE comments (post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE RESTRICT)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := validateTableForeignKeys(tx, "comments"); err == nil {
		t.Fatal("accepted a foreign key with the wrong delete action")
	}
}

func TestCheckConstraintValidationRejectsMissingCurrentConstraints(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sites (id INTEGER PRIMARY KEY CHECK (id = 1))`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := validateCheckConstraints(tx, "sites"); err == nil {
		t.Fatal("accepted a current schema missing reading-setting CHECK constraints")
	}
}

func TestCheckConstraintValidationIgnoresCommentsAndPreservesLiteralValues(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE users (
		role TEXT NOT NULL DEFAULT 'editor' /* CHECK (role IN ('owner', 'editor', 'moderator')) */,
		active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1))
	)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := validateCheckConstraints(tx, "users"); err == nil {
		t.Fatal("accepted a CHECK constraint that appears only in a SQL comment")
	}

	if got, want := normalizeCheckExpression(`status IN ('PUBLISHED')`), normalizeCheckExpression(`status IN ('published')`); got == want {
		t.Fatalf("case-sensitive string literals were normalized to the same expression %q", got)
	}
}

func TestConstraintConflictPolicyValidation(t *testing.T) {
	tests := []struct {
		name       string
		definition string
		wantErr    bool
	}{
		{
			name:       "unique constraint ignore policy",
			definition: `username TEXT NOT NULL UNIQUE ON /* policy comment */ CONFLICT IGNORE`,
			wantErr:    true,
		},
		{
			name:       "not null constraint replace policy",
			definition: `active INTEGER NOT NULL ON CONFLICT REPLACE`,
			wantErr:    true,
		},
		{
			name:       "quoted and commented text",
			definition: `"on conflict replace" TEXT DEFAULT 'ON CONFLICT IGNORE', value TEXT CHECK (instr(value, 'ON CONFLICT FAIL') = 0) /* ON CONFLICT ROLLBACK */`,
		},
		{
			name:       "foreign key action",
			definition: `post_id TEXT REFERENCES posts(id) ON DELETE CASCADE`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			definition := `CREATE TABLE posts (id TEXT PRIMARY KEY); CREATE TABLE example (` + test.definition + `)`
			if _, err := db.Exec(definition); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := validateConstraintConflictPolicies(tx, "example"); (err != nil) != test.wantErr {
				t.Fatalf("validateConstraintConflictPolicies() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestCheckExpressionParserSkipsQuotedTextAndComments(t *testing.T) {
	definition := `CREATE TABLE example (
		value TEXT /* CHECK (commented = 1) */,
		label TEXT CHECK (label IN ('CHECK (quoted)', 'MiXeD  value')) -- CHECK (line_comment = 1)
	)`
	expressions, err := extractCheckExpressions(definition)
	if err != nil {
		t.Fatal(err)
	}
	if len(expressions) != 1 {
		t.Fatalf("extracted expressions = %q, want one real CHECK", expressions)
	}
	if got, want := normalizeCheckExpression(expressions[0]), normalizeCheckExpression(`label IN ('CHECK (quoted)', 'MiXeD  value')`); got != want {
		t.Fatalf("normalized expression = %q, want %q", got, want)
	}
}

func TestPreviousSchemaVersionIsRejectedWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE preserved (value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO preserved(value) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if repo, err := NewSQLiteRepository(path); err == nil {
		_ = repo.Close()
		t.Fatal("opened a database with the previous schema version")
	} else if !strings.Contains(err.Error(), "expected schema version 2") {
		t.Fatalf("previous schema version error = %v", err)
	}

	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM preserved`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if version != 1 || value != "keep" {
		t.Fatalf("rejected database changed: version=%d value=%q", version, value)
	}
}

func tableColumns(t *testing.T, repo *SQLiteRepository, table string) map[string]bool {
	t.Helper()
	rows, err := repo.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}

func createLegacyDatabase(t *testing.T, path, postUpdatedAt string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE sites (id INTEGER PRIMARY KEY CHECK (id = 1), name TEXT NOT NULL, url TEXT NOT NULL, admin_mail TEXT NOT NULL, timezone TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, email TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'editor' CHECK (role IN ('owner', 'editor', 'moderator')), active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)), created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, slug TEXT NOT NULL UNIQUE, excerpt TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')), published_at TEXT, updated_at TEXT NOT NULL)`,
		`CREATE TABLE pages (id TEXT PRIMARY KEY, title TEXT NOT NULL, slug TEXT NOT NULL UNIQUE, excerpt TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')), published_at TEXT, updated_at TEXT NOT NULL)`,
		`CREATE TABLE terms (id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE, slug TEXT NOT NULL, kind TEXT NOT NULL CHECK (kind IN ('category', 'tag')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(kind, name), UNIQUE(kind, slug))`,
		`CREATE TABLE post_terms (post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE, term_id TEXT NOT NULL REFERENCES terms(id) ON DELETE CASCADE, PRIMARY KEY(post_id, term_id))`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE, author_name TEXT NOT NULL, author_email TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'spam')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE media (id TEXT PRIMARY KEY, filename TEXT NOT NULL, content_type TEXT NOT NULL, size INTEGER NOT NULL, storage_key TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL)`,
		`CREATE TABLE sessions (token_hash TEXT PRIMARY KEY, username TEXT NOT NULL, expires_at TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE audit_events (id TEXT PRIMARY KEY, actor_user_id TEXT NOT NULL DEFAULT '', actor_username TEXT NOT NULL, action TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL DEFAULT '', details TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE TABLE search_tokens (kind TEXT NOT NULL, token TEXT NOT NULL, record_id TEXT NOT NULL, PRIMARY KEY (kind, token, record_id))`,
		`CREATE TABLE post_revisions (id TEXT PRIMARY KEY, post_id TEXT NOT NULL, title TEXT NOT NULL, excerpt TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE outbound_mail (id TEXT PRIMARY KEY, recipient TEXT NOT NULL, subject TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE password_resets (token_hash TEXT PRIMARY KEY, username TEXT NOT NULL, expires_at TEXT NOT NULL, created_at TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	for version := 1; version <= 8; version++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, version, "2024-01-02T03:04:05.123+08:00"); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	const timestamp = "2024-11-01T01:02:03.123+08:00"
	if _, err := db.Exec(`INSERT INTO sites(id, name, url, admin_mail, timezone, created_at, updated_at) VALUES (1, 'Legacy site', 'https://example.test', 'owner@example.test', 'Asia/Shanghai', ?, ?)`, timestamp, timestamp); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id, username, password_hash, email, role, active, created_at, updated_at) VALUES ('legacy-user', 'legacy-owner', 'hash', 'owner@example.test', 'owner', 1, ?, ?)`, timestamp, timestamp); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO posts(id, title, slug, excerpt, content, status, published_at, updated_at) VALUES ('legacy-post', 'Legacy post', 'legacy-post', '', 'Keep this body', 'published', '2025-03-04T05:06:07.890123456+08:00', ?)`, postUpdatedAt); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comments(id, post_id, author_name, author_email, content, status, created_at, updated_at) VALUES ('legacy-comment', 'legacy-post', 'Old commenter', 'commenter@example.test', 'Keep this comment', 'approved', ?, ?)`, timestamp, timestamp); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(token_hash, username, expires_at, created_at) VALUES ('legacy-token', 'legacy-owner', '2030-01-02T03:04:05.123+08:00', ?)`, timestamp); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
