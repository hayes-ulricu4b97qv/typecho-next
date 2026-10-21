package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthenticatedSessionCannotSurvivePasswordChange(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	if err := repo.SetupContext(ctx,
		Site{Name: "Site", URL: "https://example.test", AdminMail: "owner@example.test", Timezone: "UTC"},
		Admin{Username: "owner", PasswordHash: "old-hash", Email: "owner@example.test"},
	); err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(time.Hour)
	if err := repo.CreateAuthenticatedSessionContext(ctx, "owner", "old-hash", "session-before-change", expiresAt); err != nil {
		t.Fatal(err)
	}

	owner, err := repo.AdminContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner.PasswordHash = "new-hash"
	if err := repo.UpdateAdminAndRevokeOtherSessionsContext(ctx, *owner, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ResolveSessionContext(ctx, "session-before-change", time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session created before password change remained valid: %v", err)
	}
	if err := repo.CreateAuthenticatedSessionContext(ctx, "owner", "old-hash", "stale-login", expiresAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session created from a stale password hash returned %v, want ErrNotFound", err)
	}
	if err := repo.CreateAuthenticatedSessionContext(ctx, "owner", "new-hash", "current-login", expiresAt); err != nil {
		t.Fatalf("session with the current password hash failed: %v", err)
	}
}
