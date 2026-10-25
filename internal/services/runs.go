package services

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
)

type Runs struct{ s *Services }

type RunView struct {
	RunID        string     `json:"run_id"`
	ConvID       string     `json:"conv_id"`
	UserID       string     `json:"user_id"`
	UserName     string     `json:"user_name"`
	ConvTitle    string     `json:"conv_title"`
	Model        string     `json:"model"`
	Status       int        `json:"status"`
	StatusText   string     `json:"status_text"`
	Active       bool       `json:"active"`
	Iterations   int        `json:"iterations"`
	PromptTokens int        `json:"prompt_tokens"`
	ComplTokens  int        `json:"completion_tokens"`
	TotalTokens  int        `json:"total_tokens"`
	Commit       string     `json:"commit,omitempty"`
	ErrorCode    string     `json:"error_code,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	DurationMS   int64      `json:"duration_ms"`
}

type RunDetail struct {
	Run      RunView          `json:"run"`
	Messages []store.Message  `json:"messages"`
	Usage    []store.UsageRow `json:"usage"`
}

type RunEventsPage struct {
	Items      []store.Event `json:"items"`
	NextCursor string        `json:"next_cursor"`
}

func runView(r store.Run) RunView {
	v := RunView{
		RunID: r.RunID, ConvID: r.ConvID, UserID: r.UserID, UserName: r.UserName, ConvTitle: r.ConvTitle,
		Model: r.Model, Status: r.Status, StatusText: r.StatusText, Active: r.Active,
		Iterations: r.Iterations, PromptTokens: r.PromptTokens, ComplTokens: r.ComplTokens,
		TotalTokens: r.PromptTokens + r.ComplTokens, Commit: r.Commit,
		ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage,
		StartedAt: r.StartedAt, EndedAt: r.EndedAt, CreatedAt: r.CreatedAt,
	}
	if r.StartedAt != nil {
		end := time.Now().UTC()
		if r.EndedAt != nil {
			end = *r.EndedAt
		}
		v.DurationMS = end.Sub(*r.StartedAt).Milliseconds()
	}
	return v
}

func (r *Runs) List(ctx context.Context, f store.RunFilters) (Page[RunView], error) {
	rows, next, err := r.s.Store.ListRuns(ctx, f)
	if err != nil {
		return Page[RunView]{}, Wrap(err, "list runs")
	}
	out := make([]RunView, 0, len(rows))
	for _, v := range rows {
		out = append(out, runView(v))
	}
	return Page[RunView]{Items: out, NextCursor: next}, nil
}

func (r *Runs) Get(ctx context.Context, runID string) (*RunDetail, error) {
	row, err := r.s.Store.RunByID(ctx, runID)
	if err != nil {
		return nil, Wrap(err, "get run")
	}
	msgs, err := r.s.Store.RunMessages(ctx, runID)
	if err != nil {
		return nil, Wrap(err, "run messages")
	}
	usage, err := r.s.Store.RunUsageRows(ctx, runID, offsetString(r.s.Cfg.Location(), time.Now()))
	if err != nil {
		return nil, Wrap(err, "run usage")
	}
	return &RunDetail{Run: runView(*row), Messages: msgs, Usage: usage}, nil
}

func (r *Runs) Events(ctx context.Context, runID string, after int64, limit int) (*RunEventsPage, error) {
	row, err := r.s.Store.RunByID(ctx, runID)
	if err != nil {
		return nil, Wrap(err, "get run")
	}
	items, err := r.s.Store.RunEvents(ctx, row.ConvID, runID, after, limit)
	if err != nil {
		return nil, Wrap(err, "run events")
	}
	next := ""
	if len(items) > 0 {
		next = itoa(items[len(items)-1].Seq)
	}
	return &RunEventsPage{Items: items, NextCursor: next}, nil
}

func (r *Runs) Messages(ctx context.Context, runID string) ([]store.Message, error) {
	if _, err := r.s.Store.RunByID(ctx, runID); err != nil {
		return nil, Wrap(err, "get run")
	}
	return r.s.Store.RunMessages(ctx, runID)
}

func (r *Runs) Usage(ctx context.Context, runID string) ([]store.UsageRow, error) {
	if _, err := r.s.Store.RunByID(ctx, runID); err != nil {
		return nil, Wrap(err, "get run")
	}
	return r.s.Store.RunUsageRows(ctx, runID, offsetString(r.s.Cfg.Location(), time.Now()))
}

func (r *Runs) Stats(ctx context.Context) (map[string]any, error) {
	now := time.Now().UTC()
	active, err := r.s.Store.ActiveRunCount(ctx)
	if err != nil {
		return nil, Wrap(err, "active runs")
	}
	counts, err := r.s.Store.RunStatusCounts(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return nil, Wrap(err, "run counts")
	}
	durations, err := r.s.Store.RunDurations(ctx, now.Add(-24*time.Hour), 500)
	if err != nil {
		return nil, Wrap(err, "run durations")
	}
	sort.Float64s(durations)
	p95 := 0.0
	if len(durations) > 0 {
		p95 = durations[int(float64(len(durations)-1)*0.95)]
	}
	return map[string]any{"active": active, "last_24h": counts, "p95_duration_seconds": p95, "sample_size": len(durations)}, nil
}

func (r *Runs) RecentFailures(ctx context.Context, limit int) ([]RunView, error) {
	rows, err := r.s.Store.RecentFailures(ctx, limit)
	if err != nil {
		return nil, Wrap(err, "recent failures")
	}
	out := make([]RunView, 0, len(rows))
	for _, v := range rows {
		out = append(out, runView(v))
	}
	return out, nil
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
