package store

import (
	"database/sql"
	"fmt"
	"time"
)

func addLegacyColumns(tx *sql.Tx) error {
	additions := []struct {
		table  string
		column string
		ddl    string
	}{
		{"sites", "description", `ALTER TABLE sites ADD COLUMN description TEXT NOT NULL DEFAULT ''`},
		{"sites", "keywords", `ALTER TABLE sites ADD COLUMN keywords TEXT NOT NULL DEFAULT ''`},
		{"sites", "posts_per_page", `ALTER TABLE sites ADD COLUMN posts_per_page INTEGER NOT NULL DEFAULT 10`},
		{"sites", "comments_enabled", `ALTER TABLE sites ADD COLUMN comments_enabled INTEGER NOT NULL DEFAULT 1`},
		{"sites", "comments_require_moderation", `ALTER TABLE sites ADD COLUMN comments_require_moderation INTEGER NOT NULL DEFAULT 1`},
		{"sites", "comments_threaded", `ALTER TABLE sites ADD COLUMN comments_threaded INTEGER NOT NULL DEFAULT 1`},
		{"posts", "allow_comment", `ALTER TABLE posts ADD COLUMN allow_comment INTEGER NOT NULL DEFAULT 1`},
		{"posts", "passphrase_hash", `ALTER TABLE posts ADD COLUMN passphrase_hash TEXT NOT NULL DEFAULT ''`},
		{"posts", "custom_fields", `ALTER TABLE posts ADD COLUMN custom_fields TEXT NOT NULL DEFAULT '{}'`},
		{"comments", "parent_id", `ALTER TABLE comments ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`},
		{"comments", "author_url", `ALTER TABLE comments ADD COLUMN author_url TEXT NOT NULL DEFAULT ''`},
		{"sessions", "user_id", `ALTER TABLE sessions ADD COLUMN user_id TEXT NOT NULL DEFAULT ''`},
		{"post_revisions", "custom_fields", `ALTER TABLE post_revisions ADD COLUMN custom_fields TEXT NOT NULL DEFAULT '{}'`},
	}
	for _, addition := range additions {
		columns, err := schemaColumnNames(tx, addition.table)
		if err != nil {
			return err
		}
		if columns[addition.column] {
			continue
		}
		if _, err := tx.Exec(addition.ddl); err != nil {
			return fmt.Errorf("add %s.%s: %w", addition.table, addition.column, err)
		}
	}
	return nil
}

type legacyTimestamp struct {
	rowID int64
	value string
}

const legacyTimestampBatchSize = 500

func normalizeLegacyTimestamps(tx *sql.Tx) error {
	columns := []struct {
		table  string
		column string
	}{
		{"schema_migrations", "applied_at"},
		{"sites", "created_at"}, {"sites", "updated_at"},
		{"users", "created_at"}, {"users", "updated_at"},
		{"posts", "published_at"}, {"posts", "updated_at"},
		{"pages", "published_at"}, {"pages", "updated_at"},
		{"terms", "created_at"}, {"terms", "updated_at"},
		{"comments", "created_at"}, {"comments", "updated_at"},
		{"media", "created_at"},
		{"sessions", "expires_at"}, {"sessions", "created_at"},
		{"audit_events", "created_at"},
		{"post_revisions", "created_at"},
		{"outbound_mail", "created_at"},
		{"password_resets", "expires_at"}, {"password_resets", "created_at"},
	}
	for _, item := range columns {
		if err := normalizeLegacyTimestampColumn(tx, item.table, item.column); err != nil {
			return err
		}
	}
	return nil
}

func normalizeLegacyTimestampColumn(tx *sql.Tx, table, column string) error {
	// Identifiers are fixed constants from normalizeLegacyTimestamps.
	baseQuery := `SELECT rowid, CAST(` + column + ` AS TEXT) FROM ` + table + ` WHERE ` + column + ` IS NOT NULL AND CAST(` + column + ` AS TEXT) <> ''`
	lastRowID := int64(-1 << 63)
	firstBatch := true
	for {
		query := baseQuery + ` AND rowid > ? ORDER BY rowid LIMIT ?`
		if firstBatch {
			query = baseQuery + ` ORDER BY rowid LIMIT ?`
		}
		var rows *sql.Rows
		var err error
		if firstBatch {
			rows, err = tx.Query(query, legacyTimestampBatchSize)
		} else {
			rows, err = tx.Query(query, lastRowID, legacyTimestampBatchSize)
		}
		if err != nil {
			return fmt.Errorf("read %s.%s values: %w", table, column, err)
		}
		values := make([]legacyTimestamp, 0, legacyTimestampBatchSize)
		for rows.Next() {
			var value legacyTimestamp
			if err := rows.Scan(&value.rowID, &value.value); err != nil {
				rows.Close()
				return fmt.Errorf("read %s.%s value: %w", table, column, err)
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate %s.%s values: %w", table, column, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close %s.%s values: %w", table, column, err)
		}
		if len(values) == 0 {
			return nil
		}
		firstBatch = false
		for _, value := range values {
			parsed, err := time.Parse(time.RFC3339Nano, value.value)
			if err != nil {
				return fmt.Errorf("normalize %s.%s rowid %d: invalid RFC3339 timestamp %q: %w", table, column, value.rowID, value.value, err)
			}
			result, err := tx.Exec(`UPDATE `+table+` SET `+column+` = ? WHERE rowid = ?`, formatTime(parsed), value.rowID)
			if err != nil {
				return fmt.Errorf("update %s.%s rowid %d: %w", table, column, value.rowID, err)
			}
			if count, err := result.RowsAffected(); err != nil {
				return fmt.Errorf("verify %s.%s rowid %d update: %w", table, column, value.rowID, err)
			} else if count != 1 {
				return fmt.Errorf("normalize %s.%s rowid %d: updated %d rows", table, column, value.rowID, count)
			}
			lastRowID = value.rowID
		}
	}
}
