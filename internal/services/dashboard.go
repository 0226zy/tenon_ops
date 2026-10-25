package services

import (
	"context"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
)

type Dashboard struct{ s *Services }

type DashboardSummary struct {
	UsersTotal     int                `json:"users_total"`
	UsersDisabled  int                `json:"users_disabled"`
	UsersNew7d     int                `json:"users_new_7d"`
	UsersUnmanaged int                `json:"users_unmanaged"`
	ActiveUsers15m int                `json:"active_users_15m"`
	ActiveRuns     int                `json:"active_runs"`
	RunStatus24h   map[string]int     `json:"run_status_24h"`
	P95Duration    float64            `json:"p95_duration_seconds"`
	TodayRequests  int64              `json:"today_requests"`
	TodayTokens    int64              `json:"today_tokens"`
	TodayCost      float64            `json:"today_cost"`
	TodayErrors    int64              `json:"today_errors"`
	ErrorRate      float64            `json:"error_rate"`
	UsageSeries    []store.UsagePoint `json:"usage_series"`
	TopModels      []store.UsageAgg   `json:"top_models"`
	TopUsers       []store.UsageAgg   `json:"top_users"`
	RecentFailures []RunView          `json:"recent_failures"`
	AsOf           time.Time          `json:"as_of"`
	DataThrough    *time.Time         `json:"data_through,omitempty"`
}

func (d *Dashboard) Summary(ctx context.Context) (*DashboardSummary, error) {
	now := time.Now().UTC()
	loc := d.s.Cfg.Location()
	localNow := now.In(loc)
	startLocal := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	start := startLocal.UTC()
	end := now.Add(time.Minute)
	totalUsers, disabled, new7d, err := d.s.Store.CountUsers(ctx)
	if err != nil {
		return nil, Wrap(err, "user counts")
	}
	unmanaged, err := d.s.Store.UnmanagedUserCount(ctx)
	if err != nil {
		return nil, Wrap(err, "unmanaged users")
	}
	activeRuns, err := d.s.Store.ActiveRunCount(ctx)
	if err != nil {
		return nil, Wrap(err, "active runs")
	}
	statusCounts, err := d.s.Store.RunStatusCounts(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return nil, Wrap(err, "run counts")
	}
	durations, err := d.s.Store.RunDurations(ctx, now.Add(-24*time.Hour), 500)
	if err != nil {
		return nil, Wrap(err, "durations")
	}
	p95 := percentile95(durations)
	presence, err := d.s.Presence.List(ctx, "15m")
	if err != nil {
		return nil, err
	}
	todayItems, today, err := d.s.Store.UsageSummary(ctx, store.UsageFilters{From: start, To: end}, "model")
	if err != nil {
		return nil, Wrap(err, "today usage")
	}
	series, err := d.s.Store.UsageSeries(ctx, store.UsageFilters{From: now.Add(-24 * time.Hour), To: end}, offsetString(loc, now), "model")
	if err != nil {
		return nil, Wrap(err, "usage series")
	}
	topModels := todayItems
	if len(topModels) > 5 {
		topModels = topModels[:5]
	}
	userItems, _, err := d.s.Store.UsageSummary(ctx, store.UsageFilters{From: start, To: end}, "user")
	if err != nil {
		return nil, Wrap(err, "top users")
	}
	if len(userItems) > 5 {
		userItems = userItems[:5]
	}
	failures, err := d.s.Runs.RecentFailures(ctx, 5)
	if err != nil {
		return nil, err
	}
	lastUsage, _ := d.s.Store.LastUsageAt(ctx)
	lastEvent, _ := d.s.Store.LastEventAt(ctx)
	dataThrough := lastUsage
	if lastEvent != nil && (dataThrough == nil || lastEvent.After(*dataThrough)) {
		dataThrough = lastEvent
	}
	return &DashboardSummary{
		UsersTotal: totalUsers, UsersDisabled: disabled, UsersNew7d: new7d, UsersUnmanaged: unmanaged,
		ActiveUsers15m: len(presence.Items), ActiveRuns: activeRuns, RunStatus24h: statusCounts, P95Duration: p95,
		TodayRequests: today.Requests, TodayTokens: today.TotalTokens, TodayCost: today.Cost, TodayErrors: today.Errors, ErrorRate: today.ErrorRate,
		UsageSeries: series, TopModels: topModels, TopUsers: userItems, RecentFailures: failures,
		AsOf: now, DataThrough: dataThrough,
	}, nil
}

func percentile95(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	// avoid importing sort in this file: copy and simple insertion sort
	v := append([]float64(nil), values...)
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
	return v[int(float64(len(v)-1)*0.95)]
}
