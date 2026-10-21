package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

const maxCommentCursorBytes = 2048

type commentCursorPayload struct {
	SnapshotAt time.Time `json:"snapshotAt"`
	BeforeAt   time.Time `json:"beforeAt"`
	BeforeID   string    `json:"beforeId"`
}

func decodeCommentCursor(raw string) (*store.CommentCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxCommentCursorBytes {
		return nil, errors.New("comment cursor is too large")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("comment cursor is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload commentCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("comment cursor is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("comment cursor is invalid")
	}
	cursor := store.CommentCursor{
		SnapshotAt: payload.SnapshotAt,
		BeforeAt:   payload.BeforeAt,
		BeforeID:   payload.BeforeID,
	}
	if !validCommentCursor(cursor) {
		return nil, errors.New("comment cursor is invalid")
	}
	return &cursor, nil
}

func encodeCommentCursor(cursor store.CommentCursor) (string, error) {
	if !validCommentCursor(cursor) {
		return "", errors.New("comment cursor is invalid")
	}
	data, err := json.Marshal(commentCursorPayload{
		SnapshotAt: cursor.SnapshotAt,
		BeforeAt:   cursor.BeforeAt,
		BeforeID:   cursor.BeforeID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func validCommentCursor(cursor store.CommentCursor) bool {
	if cursor.SnapshotAt.IsZero() || cursor.BeforeAt.IsZero() || strings.TrimSpace(cursor.BeforeID) == "" {
		return false
	}
	return !cursor.BeforeAt.After(cursor.SnapshotAt)
}
