package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func searchTokens(parts ...string) []string {
	seen := make(map[string]struct{})
	tokens := make([]string, 0)
	add := func(token string) {
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	for _, part := range parts {
		runes := []rune(strings.ToLower(part))
		for index := range runes {
			add(string(runes[index : index+1]))
			if index+1 < len(runes) {
				add(string(runes[index : index+2]))
			}
		}
	}
	return tokens
}

func searchTokenCandidatesSQL(tokenCount int) string {
	placeholders := make([]string, tokenCount)
	for index := range placeholders {
		placeholders[index] = "?"
	}
	return `SELECT record_id FROM search_tokens WHERE kind = ? AND token IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY record_id HAVING COUNT(*) = ?`
}

func searchTokenFilter(idColumn, kind, search string) (string, []any) {
	tokens := searchTokens(search)
	if len(tokens) == 0 {
		return "0", nil
	}
	args := make([]any, 0, len(tokens)+2)
	args = append(args, kind)
	for _, token := range tokens {
		args = append(args, token)
	}
	args = append(args, len(tokens))
	return idColumn + " IN (" + searchTokenCandidatesSQL(len(tokens)) + ")", args
}

func containsLiteral(parts []string, search string) bool {
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle == "" {
		return true
	}
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strings.ToLower(part))
		builder.WriteByte('\n')
	}
	return strings.Contains(builder.String(), needle)
}

func replaceSearchTokensContext(ctx context.Context, tx *sql.Tx, kind, id string, parts ...string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_tokens WHERE kind = ? AND record_id = ?`, kind, id); err != nil {
		return fmt.Errorf("clear search tokens: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO search_tokens(kind, token, record_id) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare search tokens: %w", err)
	}
	defer statement.Close()
	for _, token := range searchTokens(parts...) {
		if _, err := statement.ExecContext(ctx, kind, token, id); err != nil {
			return fmt.Errorf("write search token: %w", err)
		}
	}
	return nil
}

func deleteSearchTokensContext(ctx context.Context, tx *sql.Tx, kind, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_tokens WHERE kind = ? AND record_id = ?`, kind, id); err != nil {
		return fmt.Errorf("delete search tokens: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) matchingSearchPageIDsContext(ctx context.Context, table, where, orderBy string, args []any, search string, page, pageSize int) ([]string, int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, title, excerpt, slug, content FROM `+table+` WHERE `+where+` ORDER BY `+orderBy, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("read search candidates: %w", err)
	}
	defer rows.Close()
	offset := pageOffset(page, pageSize)
	ids := make([]string, 0, pageSize)
	total := 0
	for rows.Next() {
		var id, title, excerpt, slug, content string
		if err := rows.Scan(&id, &title, &excerpt, &slug, &content); err != nil {
			return nil, 0, fmt.Errorf("scan search candidate: %w", err)
		}
		if !containsLiteral([]string{title, excerpt, slug, content}, search) {
			continue
		}
		if total >= offset && len(ids) < pageSize {
			ids = append(ids, id)
		}
		total++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate search candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, fmt.Errorf("close search candidates: %w", err)
	}
	return ids, total, nil
}
