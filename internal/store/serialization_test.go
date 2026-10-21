package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrivateRepositoryFieldsAreNotJSONByDefault(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		secret string
	}{
		{name: "site mail", value: Site{AdminMail: "site-secret@example.com"}, secret: "site-secret@example.com"},
		{name: "password hash", value: User{PasswordHash: "bcrypt-secret"}, secret: "bcrypt-secret"},
		{name: "comment email", value: Comment{AuthorEmail: "comment-secret@example.com"}, secret: "comment-secret@example.com"},
		{name: "media storage key", value: Media{StorageKey: "storage-secret-key"}, secret: "storage-secret-key"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), test.secret) {
				t.Fatalf("JSON payload exposed private value %q: %s", test.secret, encoded)
			}
		})
	}
}
