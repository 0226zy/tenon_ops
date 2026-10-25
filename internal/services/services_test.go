package services

import (
	"testing"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
)

func TestOffsetString(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	if got := offsetString(loc, time.Now()); got != "+08:00" {
		t.Fatalf("offset = %q", got)
	}
}

func TestTier(t *testing.T) {
	now := time.Now().UTC()
	seen := now.Add(-2 * time.Minute)
	if got := tier(now, presenceRow(seen, 0)); got != "online" {
		t.Fatalf("online tier = %q", got)
	}
	if got := tier(now, presenceRow(now.Add(-10*time.Minute), 0)); got != "active" {
		t.Fatalf("active tier = %q", got)
	}
	if got := tier(now, presenceRow(seen, 1)); got != "running" {
		t.Fatalf("running tier = %q", got)
	}
}

func presenceRow(seen time.Time, active int) store.PresenceRow {
	return store.PresenceRow{LastSeen: &seen, ActiveRuns: active}
}

func TestCSVSafe(t *testing.T) {
	if csvSafe("=cmd") != "'=cmd" || csvSafe("normal") != "normal" {
		t.Fatal("csvSafe")
	}
}
