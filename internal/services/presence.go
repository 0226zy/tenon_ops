package services

import (
	"context"
	"sync"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
)

type Presence struct {
	s     *Services
	mu    sync.Mutex
	cache presenceCache
}

type presenceCache struct {
	at     time.Time
	window time.Duration
	items  []PresenceView
}

type PresenceView struct {
	UserID       string     `json:"user_id"`
	Name         string     `json:"name"`
	Status       int        `json:"status"`
	Tier         string     `json:"tier"`
	LastSeen     *time.Time `json:"last_seen,omitempty"`
	ActiveRuns   int        `json:"active_runs"`
	Sessions     int        `json:"sessions"`
	TokensWindow int64      `json:"tokens_window"`
	Tokens24h    int64      `json:"tokens_24h"`
	Model        string     `json:"model,omitempty"`
}

type PresenceResult struct {
	AsOf          time.Time      `json:"as_of"`
	WindowSeconds int64          `json:"window_seconds"`
	Items         []PresenceView `json:"items"`
}

func (p *Presence) List(ctx context.Context, window string) (*PresenceResult, error) {
	d, err := parseWindow(window)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if time.Since(p.cache.at) < 5*time.Second && p.cache.window == d {
		items := p.cache.items
		p.mu.Unlock()
		return &PresenceResult{AsOf: p.cache.at, WindowSeconds: int64(d.Seconds()), Items: items}, nil
	}
	p.mu.Unlock()
	rows, err := p.s.Store.Presence(ctx, d, 200)
	if err != nil {
		return nil, Wrap(err, "presence")
	}
	now := time.Now().UTC()
	items := make([]PresenceView, 0, len(rows))
	for _, r := range rows {
		items = append(items, PresenceView{
			UserID: r.UserID, Name: r.Name, Status: r.Status, Tier: tier(now, r),
			LastSeen: r.LastSeen, ActiveRuns: r.ActiveRuns, Sessions: r.Sessions,
			TokensWindow: r.TokensWindow, Tokens24h: r.Tokens24h, Model: r.Model,
		})
	}
	p.mu.Lock()
	p.cache = presenceCache{at: now, window: d, items: items}
	p.mu.Unlock()
	return &PresenceResult{AsOf: now, WindowSeconds: int64(d.Seconds()), Items: items}, nil
}

func parseWindow(v string) (time.Duration, error) {
	if v == "" {
		return 15 * time.Minute, nil
	}
	switch v {
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "60m", "1h":
		return time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Minute || d > 24*time.Hour {
		return 0, BadRequest("BAD_WINDOW", "window must be 5m, 15m, 60m or 24h")
	}
	return d, nil
}

func tier(now time.Time, r store.PresenceRow) string {
	if r.ActiveRuns > 0 {
		return "running"
	}
	if r.LastSeen == nil {
		return "idle"
	}
	age := now.Sub(*r.LastSeen)
	switch {
	case age <= 5*time.Minute:
		return "online"
	case age <= 15*time.Minute:
		return "active"
	case age <= time.Hour:
		return "recent"
	case age <= 24*time.Hour:
		return "today"
	default:
		return "idle"
	}
}
