package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type UsageFilters struct {
	From   time.Time
	To     time.Time
	UserID string
	Model  string
	ConvID string
	Limit  int
	Offset string // display/business timezone offset, e.g. +08:00
}

func (f UsageFilters) offset() string {
	v := f.Offset
	if len(v) != 6 || (v[0] != '+' && v[0] != '-') || v[3] != ':' {
		return "+08:00"
	}
	for _, i := range []int{1, 2, 4, 5} {
		if v[i] < '0' || v[i] > '9' {
			return "+08:00"
		}
	}
	return v
}

func (f UsageFilters) where(alias string) (string, []any) {
	where := []string{fmt.Sprintf("%s.Fcreate_time>=?", alias), fmt.Sprintf("%s.Fcreate_time<?", alias)}
	args := []any{f.From.UTC(), f.To.UTC()}
	if f.UserID != "" {
		where = append(where, fmt.Sprintf("%s.Fuser_id=?", alias))
		args = append(args, f.UserID)
	}
	if f.Model != "" {
		where = append(where, fmt.Sprintf("%s.Fmodel=?", alias))
		args = append(args, f.Model)
	}
	if f.ConvID != "" {
		where = append(where, fmt.Sprintf("%s.Fconv_id=?", alias))
		args = append(args, f.ConvID)
	}
	return strings.Join(where, " AND "), args
}

// priceJoin attaches the price effective at each usage row.
func (s *Store) priceJoin() string {
	p := s.a("tadm_model_price")
	return ` LEFT JOIN ` + p + ` p ON p.Fmodel_id=u.Fmodel AND p.Feffective_from=(
		SELECT MAX(p2.Feffective_from) FROM ` + p + ` p2 WHERE p2.Fmodel_id=u.Fmodel AND p2.Feffective_from<=u.Fcreate_time)`
}

// peakExpr is true for Mon-Fri 09:00-12:00 and 14:00-18:00 in the business
// timezone. Weekends and all other hours are off-peak.
func (s *Store) peakExpr(f UsageFilters) string {
	off := f.offset()
	local := `CONVERT_TZ(u.Fcreate_time,'+00:00','` + off + `')`
	t := `TIME(` + local + `)`
	return `(WEEKDAY(` + local + `)<5 AND ((` + t + `>='09:00:00' AND ` + t + `<'12:00:00') OR (` + t + `>='14:00:00' AND ` + t + `<'18:00:00')))`
}

// cacheHitExpr returns cached prompt tokens when Tenon exposes them. The
// current t_llm_usage schema has no such column, so it falls back to 0 (all
// prompt tokens are treated as cache misses).
func (s *Store) cacheHitExpr(alias string) string {
	if s.usageCacheColumn == "" {
		return "0"
	}
	return "LEAST(" + alias + ".Fprompt_tokens,GREATEST(" + alias + "." + s.usageCacheColumn + ",0))"
}

func (s *Store) costExpr(f UsageFilters) string {
	hit := s.cacheHitExpr("u")
	return `COALESCE((CASE WHEN ` + s.peakExpr(f) + ` THEN p.Fpeak_multiplier ELSE 1 END) * (((u.Fprompt_tokens-` + hit + `)/1000000)*p.Finput_per_mtok + (` + hit + `/1000000)*p.Fcache_hit_per_mtok + (u.Fcompl_tokens/1000000)*p.Foutput_per_mtok),0)`
}

const errExpr = `CASE WHEN u.Fhttp_status=0 OR u.Fhttp_status>=400 THEN 1 ELSE 0 END`
const pricedExpr = `CASE WHEN p.Fid IS NULL THEN 0 ELSE 1 END`

func (s *Store) UsageSummary(ctx context.Context, f UsageFilters, groupBy string) ([]UsageAgg, UsageAgg, error) {
	where, args := f.where("u")
	groupExpr := ""
	groupField := ""
	switch groupBy {
	case "user":
		groupExpr, groupField = "u.Fuser_id", "user"
	case "conv":
		groupExpr, groupField = "u.Fconv_id", "conv"
	default:
		groupExpr, groupField = "u.Fmodel", "model"
	}
	cost := s.costExpr(f)
	q := `SELECT ` + groupExpr + `,COUNT(*),COALESCE(SUM(u.Fprompt_tokens),0),COALESCE(SUM(u.Fcompl_tokens),0),COALESCE(SUM(` + errExpr + `),0),COALESCE(SUM(` + cost + `),0),COALESCE(SUM(` + pricedExpr + `),0),COALESCE(AVG(u.Fcost_ms),0)
		FROM ` + s.t("t_llm_usage") + ` u ` + s.priceJoin() + `
		WHERE ` + where + ` GROUP BY ` + groupExpr + ` ORDER BY COALESCE(SUM(u.Fprompt_tokens+u.Fcompl_tokens),0) DESC LIMIT ?`
	args = append(args, Limit(f.Limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, UsageAgg{}, err
	}
	defer rows.Close()
	items := []UsageAgg{}
	for rows.Next() {
		var key string
		var a UsageAgg
		if err := rows.Scan(&key, &a.Requests, &a.PromptTokens, &a.ComplTokens, &a.Errors, &a.Cost, &a.PricedRequests, &a.AvgMS); err != nil {
			return nil, UsageAgg{}, err
		}
		a.TotalTokens = a.PromptTokens + a.ComplTokens
		a.UnpricedRequests = a.Requests - a.PricedRequests
		if a.Requests > 0 {
			a.ErrorRate = float64(a.Errors) / float64(a.Requests)
		}
		switch groupField {
		case "user":
			a.UserID = key
		case "conv":
			a.ConvID = key
		default:
			a.Model = key
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return nil, UsageAgg{}, err
	}
	totalQ := `SELECT COUNT(*),COALESCE(SUM(u.Fprompt_tokens),0),COALESCE(SUM(u.Fcompl_tokens),0),COALESCE(SUM(` + errExpr + `),0),COALESCE(SUM(` + cost + `),0),COALESCE(SUM(` + pricedExpr + `),0),COALESCE(AVG(u.Fcost_ms),0)
		FROM ` + s.t("t_llm_usage") + ` u ` + s.priceJoin() + ` WHERE ` + where
	var total UsageAgg
	if err := s.db.QueryRowContext(ctx, totalQ, args[:len(args)-1]...).Scan(&total.Requests, &total.PromptTokens, &total.ComplTokens, &total.Errors, &total.Cost, &total.PricedRequests, &total.AvgMS); err != nil {
		return nil, UsageAgg{}, err
	}
	total.TotalTokens = total.PromptTokens + total.ComplTokens
	total.UnpricedRequests = total.Requests - total.PricedRequests
	if total.Requests > 0 {
		total.ErrorRate = float64(total.Errors) / float64(total.Requests)
	}
	return items, total, nil
}

func (s *Store) UsageSeries(ctx context.Context, f UsageFilters, offset string, groupBy string) ([]UsagePoint, error) {
	f.Offset = offset
	where, args := f.where("u")
	cost := s.costExpr(f)
	modelExpr := "''"
	modelGroup := ""
	if groupBy == "model" {
		modelExpr = "u.Fmodel"
		modelGroup = ",u.Fmodel"
	}
	q := `SELECT DATE(CONVERT_TZ(u.Fcreate_time,'+00:00',?)) AS d,` + modelExpr + `,COUNT(*),COALESCE(SUM(u.Fprompt_tokens+u.Fcompl_tokens),0),COALESCE(SUM(` + errExpr + `),0),COALESCE(SUM(` + cost + `),0)
		FROM ` + s.t("t_llm_usage") + ` u ` + s.priceJoin() + `
		WHERE ` + where + ` GROUP BY d` + modelGroup + ` ORDER BY d`
	args2 := append([]any{offset}, args...)
	rows, err := s.db.QueryContext(ctx, q, args2...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsagePoint{}
	for rows.Next() {
		var p UsagePoint
		var day time.Time
		if err := rows.Scan(&day, &p.Model, &p.Requests, &p.Tokens, &p.Errors, &p.Cost); err != nil {
			return nil, err
		}
		p.Day = day.Format("2006-01-02")
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UsageBreakdown(ctx context.Context, f UsageFilters, dimension string, limit int) ([]UsageAgg, error) {
	items, _, err := s.UsageSummary(ctx, f, dimension)
	if err != nil {
		return nil, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

type UsageRow struct {
	CreatedAt  time.Time `json:"created_at"`
	UserID     string    `json:"user_id"`
	ConvID     string    `json:"conv_id"`
	RunID      string    `json:"run_id"`
	Model      string    `json:"model"`
	Prompt     int64     `json:"prompt_tokens"`
	Completion int64     `json:"completion_tokens"`
	CostMS     int       `json:"cost_ms"`
	HTTPStatus int       `json:"http_status"`
	Cost       float64   `json:"cost"`
	Priced     bool      `json:"priced"`
}

func (s *Store) UsageRows(ctx context.Context, f UsageFilters, limit int) ([]UsageRow, error) {
	where, args := f.where("u")
	cost := s.costExpr(f)
	limit = Limit(limit, 5000, 50000)
	q := `SELECT u.Fcreate_time,u.Fuser_id,u.Fconv_id,u.Frun_id,u.Fmodel,u.Fprompt_tokens,u.Fcompl_tokens,u.Fcost_ms,u.Fhttp_status,` + cost + `,` + pricedExpr + `
		FROM ` + s.t("t_llm_usage") + ` u ` + s.priceJoin() + ` WHERE ` + where + ` ORDER BY u.Fid DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageRow{}
	for rows.Next() {
		var v UsageRow
		var priced int
		if err := rows.Scan(&v.CreatedAt, &v.UserID, &v.ConvID, &v.RunID, &v.Model, &v.Prompt, &v.Completion, &v.CostMS, &v.HTTPStatus, &v.Cost, &priced); err != nil {
			return nil, err
		}
		v.Priced = priced == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UsageFreshness(ctx context.Context) (*time.Time, error) {
	return s.LastUsageAt(ctx)
}
