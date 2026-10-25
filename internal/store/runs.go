package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

func RunStatusText(code int) string {
	switch code {
	case 0:
		return "queued"
	case 1:
		return "running"
	case 2:
		return "finished"
	case 3:
		return "failed"
	case 4:
		return "cancelled"
	case 5:
		return "dispatching"
	case 6:
		return "cancelling"
	case 7:
		return "finalizing"
	default:
		return "unknown"
	}
}

const runSelect = `r.Fid,r.Frun_id,r.Fconv_id,r.Fuser_id,COALESCE(u.Fname,r.Fuser_id),COALESCE(c.Ftitle,''),r.Fmodel,r.Fstatus,r.Factive,r.Fgeneration,r.Fcancel_req,r.Fstop_confirmed,r.Fcommit,r.Fiterations,r.Fprompt_tokens,r.Fcompl_tokens,r.Ferror_code,r.Ferror_msg,r.Fstart_time,r.Fend_time,r.Fcreate_time,r.Fmodify_time`

func scanRun(row interface{ Scan(...any) error }) (*Run, error) {
	var v Run
	var active, cancel, stop sql.NullInt64
	var start, end sql.NullTime
	err := row.Scan(&v.ID, &v.RunID, &v.ConvID, &v.UserID, &v.UserName, &v.ConvTitle, &v.Model, &v.Status, &active, &v.Generation, &cancel, &stop, &v.Commit, &v.Iterations, &v.PromptTokens, &v.ComplTokens, &v.ErrorCode, &v.ErrorMessage, &start, &end, &v.CreatedAt, &v.ModifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Active = active.Valid && active.Int64 == 1
	v.CancelReq = cancel.Valid && cancel.Int64 == 1
	v.StopConfirmed = stop.Valid && stop.Int64 == 1
	v.StartedAt = timePtr(start)
	v.EndedAt = timePtr(end)
	v.StatusText = RunStatusText(v.Status)
	return &v, nil
}

func (s *Store) ListRuns(ctx context.Context, f RunFilters) ([]Run, string, error) {
	limit := Limit(f.Limit, 50, 100)
	where := []string{"1=1"}
	var args []any
	switch f.Status {
	case "active", "running", "queued":
		where = append(where, "r.Factive=1")
		if f.Status == "queued" {
			where = append(where, "r.Fstatus IN (0,5)")
		}
	case "failed":
		where = append(where, "r.Fstatus=3")
	case "finished":
		where = append(where, "r.Fstatus=2")
	case "cancelled":
		where = append(where, "r.Fstatus=4")
	}
	if f.UserID != "" {
		where = append(where, "r.Fuser_id=?")
		args = append(args, f.UserID)
	}
	if f.Model != "" {
		where = append(where, "r.Fmodel=?")
		args = append(args, f.Model)
	}
	if f.From != nil {
		where = append(where, "r.Fcreate_time>=?")
		args = append(args, f.From.UTC())
	}
	if f.To != nil {
		where = append(where, "r.Fcreate_time<?")
		args = append(args, f.To.UTC())
	}
	if f.Cursor != "" {
		id, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil {
			return nil, "", err
		}
		where = append(where, "r.Fid<?")
		args = append(args, id)
	}
	args = append(args, limit+1)
	q := `SELECT ` + runSelect + ` FROM ` + s.t("t_run") + ` r
		LEFT JOIN ` + s.t("t_user") + ` u ON u.Fuser_id=r.Fuser_id
		LEFT JOIN ` + s.t("t_conversation") + ` c ON c.Fconv_id=r.Fconv_id
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY r.Fid DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		v, err := scanRun(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = strconv.FormatInt(out[len(out)-1].ID, 10)
	}
	return out, next, nil
}

func (s *Store) RunByID(ctx context.Context, runID string) (*Run, error) {
	q := `SELECT ` + runSelect + ` FROM ` + s.t("t_run") + ` r
		LEFT JOIN ` + s.t("t_user") + ` u ON u.Fuser_id=r.Fuser_id
		LEFT JOIN ` + s.t("t_conversation") + ` c ON c.Fconv_id=r.Fconv_id
		WHERE r.Frun_id=?`
	return scanRun(s.db.QueryRowContext(ctx, q, runID))
}

func (s *Store) RunEvents(ctx context.Context, convID, runID string, afterSeq int64, limit int) ([]Event, error) {
	limit = Limit(limit, 100, 200)
	rows, err := s.db.QueryContext(ctx, `SELECT Fseq,Frun_id,Fsource,Ftype,Fpayload,Fcreate_time FROM `+s.t("t_event")+`
		WHERE Fconv_id=? AND Frun_id=? AND Fseq>? ORDER BY Fseq LIMIT ?`, convID, runID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		var raw []byte
		if err := rows.Scan(&v.Seq, &v.RunID, &v.Source, &v.Type, &raw, &v.CreatedAt); err != nil {
			return nil, err
		}
		const maxPayload = 64 * 1024
		if len(raw) > maxPayload {
			// Keep the JSON envelope valid by returning the prefix as a string.
			quoted, _ := json.Marshal(string(raw[:maxPayload]))
			v.Payload = quoted
			v.Truncated = true
		} else {
			v.Payload = json.RawMessage(raw)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) RunMessages(ctx context.Context, runID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT Fmsg_id,Frole,Fcontent,Fcreate_time FROM `+s.t("t_message")+` WHERE Frun_id=? ORDER BY Fid`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var v Message
		if err := rows.Scan(&v.MsgID, &v.Role, &v.Content, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) RunUsageRows(ctx context.Context, runID, offset string) ([]UsageRow, error) {
	cost := s.costExpr(UsageFilters{Offset: offset})
	rows, err := s.db.QueryContext(ctx, `SELECT u.Fcreate_time,u.Fuser_id,u.Fconv_id,u.Frun_id,u.Fmodel,u.Fprompt_tokens,u.Fcompl_tokens,u.Fcost_ms,u.Fhttp_status,`+cost+`,`+pricedExpr+`
		FROM `+s.t("t_llm_usage")+` u `+s.priceJoin()+` WHERE u.Frun_id=? ORDER BY u.Fid`, runID)
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

func (s *Store) ActiveRunCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.t("t_run")+` WHERE Factive=1`).Scan(&n)
	return n, err
}

func (s *Store) RunStatusCounts(ctx context.Context, from time.Time) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT Fstatus,COUNT(*) FROM `+s.t("t_run")+` WHERE Fcreate_time>=? GROUP BY Fstatus`, from.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status, n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[RunStatusText(status)] = n
	}
	return out, rows.Err()
}

func (s *Store) RunDurations(ctx context.Context, from time.Time, limit int) ([]float64, error) {
	limit = Limit(limit, 500, 2000)
	rows, err := s.db.QueryContext(ctx, `SELECT TIMESTAMPDIFF(SECOND,Fstart_time,Fend_time) FROM `+s.t("t_run")+`
		WHERE Fcreate_time>=? AND Fstart_time IS NOT NULL AND Fend_time IS NOT NULL AND Factive IS NULL ORDER BY Fid DESC LIMIT ?`, from.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []float64{}
	for rows.Next() {
		var n sql.NullInt64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if n.Valid {
			out = append(out, float64(n.Int64))
		}
	}
	return out, rows.Err()
}

func (s *Store) RecentFailures(ctx context.Context, limit int) ([]Run, error) {
	limit = Limit(limit, 5, 20)
	q := `SELECT ` + runSelect + ` FROM ` + s.t("t_run") + ` r
		LEFT JOIN ` + s.t("t_user") + ` u ON u.Fuser_id=r.Fuser_id
		LEFT JOIN ` + s.t("t_conversation") + ` c ON c.Fconv_id=r.Fconv_id
		WHERE r.Fstatus=3 ORDER BY r.Fid DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		v, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
