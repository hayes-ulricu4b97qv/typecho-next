package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountUpdatesRejectStaleSnapshotsAfterPasswordReset(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	if err := repo.SetupContext(ctx,
		Site{Name: "Site", URL: "https://example.test", AdminMail: "owner@example.test", Timezone: "UTC"},
		Admin{Username: "owner", PasswordHash: "owner-old-hash", Email: "owner@example.test"},
	); err != nil {
		t.Fatal(err)
	}
	staff, err := repo.CreateUserContext(ctx, User{
		Username: "staff", PasswordHash: "staff-old-hash", Email: "staff@example.test", Role: "editor",
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerSnapshot, err := repo.AdminContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	staffSnapshot, err := repo.UserContext(ctx, staff.Username)
	if err != nil {
		t.Fatal(err)
	}

	resetAt := time.Now().UTC().Add(time.Second)
	for _, account := range []struct {
		username string
		hash     string
	}{
		{username: "owner", hash: "owner-reset-hash"},
		{username: "staff", hash: "staff-reset-hash"},
	} {
		tokenHash := HashResetToken(account.username + "-reset-token")
		if err := repo.SavePasswordResetContext(ctx, account.username, tokenHash, resetAt.Add(time.Hour)); err != nil {
			t.Fatalf("save %s reset token: %v", account.username, err)
		}
		if err := repo.CompletePasswordReset(ctx, tokenHash, resetAt, account.hash); err != nil {
			t.Fatalf("complete %s password reset: %v", account.username, err)
		}
	}

	ownerSnapshot.Email = "changed-owner@example.test"
	if err := repo.UpdateAdminContext(ctx, *ownerSnapshot); !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("stale owner update error = %v, want ErrStaleWrite", err)
	}
	ownerSnapshot.PasswordHash = "owner-late-hash"
	if err := repo.UpdateAdminAndRevokeOtherSessionsContext(ctx, *ownerSnapshot, ""); !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("stale owner password update error = %v, want ErrStaleWrite", err)
	}
	staffSnapshot.Email = "changed-staff@example.test"
	if _, err := repo.UpdateUserContext(ctx, staffSnapshot); !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("stale staff update error = %v, want ErrStaleWrite", err)
	}
	staffSnapshot.PasswordHash = "staff-late-hash"
	if _, err := repo.UpdateUserAndRevokeSessionsContext(ctx, staffSnapshot); !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("stale staff password update error = %v, want ErrStaleWrite", err)
	}

	owner, err := repo.AdminContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if owner.PasswordHash != "owner-reset-hash" || owner.Email != "owner@example.test" {
		t.Fatalf("owner after rejected stale update = %#v", owner)
	}
	staff, err = repo.UserContext(ctx, "staff")
	if err != nil {
		t.Fatal(err)
	}
	if staff.PasswordHash != "staff-reset-hash" || staff.Email != "staff@example.test" {
		t.Fatalf("staff after rejected stale update = %#v", staff)
	}
}
