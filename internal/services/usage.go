package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
)

type Usage struct{ s *Services }

type UsageOverview struct {
	From    time.Time          `json:"from"`
	To      time.Time          `json:"to"`
	Total   store.UsageAgg     `json:"total"`
	Items   []store.UsageAgg   `json:"items"`
	Series  []store.UsagePoint `json:"series"`
	GroupBy string             `json:"group_by"`
}

func (u *Usage) Range(fromText, toText string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	to := now.Add(time.Minute)
	from := now.Add(-24 * time.Hour)
	var err error
	if fromText != "" {
		from, err = parseTime(fromText)
		if err != nil {
			return time.Time{}, time.Time{}, BadRequest("BAD_TIME", "invalid from")
		}
	}
	if toText != "" {
		to, err = parseTime(toText)
		if err != nil {
			return time.Time{}, time.Time{}, BadRequest("BAD_TIME", "invalid to")
		}
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, BadRequest("BAD_TIME", "from must be before to")
	}
	if to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, BadRequest("BAD_TIME", "range must be at most 366 days")
	}
	return from, to, nil
}

func parseTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid time")
}

func (u *Usage) Overview(ctx context.Context, f store.UsageFilters, groupBy string) (*UsageOverview, error) {
	if groupBy == "" {
		groupBy = "model"
	}
	if groupBy != "model" && groupBy != "user" && groupBy != "conv" {
		return nil, BadRequest("BAD_GROUP", "group_by must be model, user or conv")
	}
	items, total, err := u.s.Store.UsageSummary(ctx, f, groupBy)
	if err != nil {
		return nil, Wrap(err, "usage summary")
	}
	offset := offsetString(u.s.Cfg.Location(), f.From)
	series, err := u.s.Store.UsageSeries(ctx, f, offset, groupBy)
	if err != nil {
		return nil, Wrap(err, "usage series")
	}
	return &UsageOverview{From: f.From, To: f.To, Total: total, Items: items, Series: series, GroupBy: groupBy}, nil
}

func (u *Usage) ExportCSV(ctx context.Context, f store.UsageFilters, meta Meta) ([]byte, int, error) {
	rows, err := u.s.Store.UsageRows(ctx, f, 50000)
	if err != nil {
		return nil, 0, Wrap(err, "usage rows")
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"created_at", "user_id", "conv_id", "run_id", "model", "prompt_tokens", "completion_tokens", "cost_ms", "http_status", "cost", "priced"})
	for _, r := range rows {
		_ = w.Write([]string{
			r.CreatedAt.UTC().Format(time.RFC3339), csvSafe(r.UserID), r.ConvID, r.RunID, r.Model,
			strconv.FormatInt(r.Prompt, 10), strconv.FormatInt(r.Completion, 10), strconv.Itoa(r.CostMS), strconv.Itoa(r.HTTPStatus),
			fmt.Sprintf("%.6f", r.Cost), strconv.FormatBool(r.Priced),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, 0, err
	}
	if err := u.s.audit(ctx, meta, "usage.export", "usage", "", 1, "", map[string]any{"from": f.From, "to": f.To, "rows": len(rows)}); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), len(rows), nil
}

func (u *Usage) Summary(ctx context.Context, f store.UsageFilters) (store.UsageAgg, error) {
	_, total, err := u.s.Store.UsageSummary(ctx, f, "model")
	return total, err
}

func offsetString(loc *time.Location, at time.Time) string {
	_, offset := at.In(loc).Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	return fmt.Sprintf("%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
}

func csvSafe(v string) string {
	if v == "" {
		return v
	}
	if strings.ContainsAny(v[:1], "=+-@") {
		return "'" + v
	}
	return v
}
