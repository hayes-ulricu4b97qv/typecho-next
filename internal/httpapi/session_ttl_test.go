package httpapi

import (
	"testing"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
)

func TestSessionTTLDoesNotOverflowRuntimeSettings(t *testing.T) {
	server := &Server{settings: config.Settings{SessionTTLMin: int(^uint(0) >> 1)}}
	if got := server.sessionTTL(); got != 24*time.Hour {
		t.Fatalf("overflowing session ttl = %s, want 24h fallback", got)
	}
}
