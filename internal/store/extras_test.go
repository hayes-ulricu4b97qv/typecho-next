package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOutboundMailRetentionAndPageMetadata(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.SaveOutboundMail("owner@example.com", "过期通知", "不应长期保留"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE outbound_mail SET created_at = ? WHERE subject = ?`, "2000-01-01T00:00:00Z", "过期通知"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if err := repo.SaveOutboundMail("owner@example.com", "通知", "内容"); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repo.ListOutboundMail(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Page != 2 || page.PageSize != 2 || len(page.Items) != 1 {
		t.Fatalf("notification page = %#v", page)
	}
	for _, item := range page.Items {
		if item.Subject == "过期通知" {
			t.Fatal("expired notification was retained")
		}
	}
}

func TestOutboundMailHistoryIsCapped(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for index := 0; index < 1001; index++ {
		if err := repo.SaveOutboundMail("owner@example.com", "通知", "内容"); err != nil {
			t.Fatalf("save notification %d: %v", index, err)
		}
	}
	page, err := repo.ListOutboundMail(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1000 || len(page.Items) != 100 {
		t.Fatalf("notification cap page total=%d items=%d", page.Total, len(page.Items))
	}
}

func TestPasswordResetReplacementAndDeletion(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Now().UTC()
	first := HashResetToken("first-token")
	second := HashResetToken("second-token")
	if err := repo.SavePasswordReset("owner", first, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePasswordReset("owner", second, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumePasswordReset(first, now); err != ErrNotFound {
		t.Fatalf("superseded reset token error = %v", err)
	}
	if err := repo.DeletePasswordReset(second); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumePasswordReset(second, now); err != ErrNotFound {
		t.Fatalf("deleted reset token error = %v", err)
	}
}
