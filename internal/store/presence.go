package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Store) Presence(ctx context.Context, window time.Duration, limit int) ([]PresenceRow, error) {
	from := utcNow().Add(-window)
	limit = Limit(limit, 200, 500)
	q := `SELECT x.user_id,MAX(x.ts) AS last_seen,COALESCE(SUM(x.tokens),0) AS tokens
		FROM (
			SELECT Fuser_id AS user_id,Flast_active_time AS ts,0 AS tokens FROM ` + s.t("t_conversation") + ` WHERE Flast_active_time>=?
			UNION ALL
			SELECT Fuser_id,Fcreate_time,0 FROM ` + s.t("t_run") + ` WHERE Fcreate_time>=?
			UNION ALL
			SELECT Fuser_id,Fcreate_time,Fprompt_tokens+Fcompl_tokens FROM ` + s.t("t_llm_usage") + ` WHERE Fcreate_time>=?
			UNION ALL
			SELECT Fuser_id,Fcreate_time,0 FROM ` + s.t("t_run") + ` WHERE Factive=1
		) x
		GROUP BY x.user_id ORDER BY last_seen DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, from, from, from, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PresenceRow{}
	ids := []string{}
	for rows.Next() {
		var id string
		var seen sql.NullTime
		var tokens int64
		if err := rows.Scan(&id, &seen, &tokens); err != nil {
			return nil, err
		}
		out = append(out, PresenceRow{UserID: id, LastSeen: timePtr(seen), TokensWindow: tokens})
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return out, err
	}
	// user names/status
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	in := placeholders(len(ids))
	userRows, err := s.db.QueryContext(ctx, `SELECT Fuser_id,Fname,Fstatus FROM `+s.t("t_user")+` WHERE Fuser_id IN (`+in+`)`, args...)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	statuses := map[string]int{}
	for userRows.Next() {
		var id, name string
		var status int
		if err := userRows.Scan(&id, &name, &status); err != nil {
			userRows.Close()
			return nil, err
		}
		names[id], statuses[id] = name, status
	}
	userRows.Close()
	// active runs
	activeRows, err := s.db.QueryContext(ctx, `SELECT Fuser_id,COUNT(*) FROM `+s.t("t_run")+` WHERE Factive=1 AND Fuser_id IN (`+in+`) GROUP BY Fuser_id`, args...)
	if err != nil {
		return nil, err
	}
	active := map[string]int{}
	for activeRows.Next() {
		var id string
		var n int
		if err := activeRows.Scan(&id, &n); err != nil {
			activeRows.Close()
			return nil, err
		}
		active[id] = n
	}
	activeRows.Close()
	// live sessions
	sessRows, err := s.db.QueryContext(ctx, `SELECT Fuser_id,COUNT(*) FROM `+s.t("t_auth_session")+` WHERE Fexpire_time>? AND Fuser_id IN (`+in+`) GROUP BY Fuser_id`, append([]any{utcNow()}, args...)...)
	if err != nil {
		return nil, err
	}
	sessions := map[string]int{}
	for sessRows.Next() {
		var id string
		var n int
		if err := sessRows.Scan(&id, &n); err != nil {
			sessRows.Close()
			return nil, err
		}
		sessions[id] = n
	}
	sessRows.Close()
	// 24h tokens
	twRows, err := s.db.QueryContext(ctx, `SELECT Fuser_id,COALESCE(SUM(Fprompt_tokens+Fcompl_tokens),0) FROM `+s.t("t_llm_usage")+` WHERE Fcreate_time>=? AND Fuser_id IN (`+in+`) GROUP BY Fuser_id`, append([]any{utcNow().Add(-24 * time.Hour)}, args...)...)
	if err != nil {
		return nil, err
	}
	tokens24 := map[string]int64{}
	for twRows.Next() {
		var id string
		var n int64
		if err := twRows.Scan(&id, &n); err != nil {
			twRows.Close()
			return nil, err
		}
		tokens24[id] = n
	}
	twRows.Close()
	// latest model per user
	modelRows, err := s.db.QueryContext(ctx, `SELECT Fuser_id,Fmodel FROM (
		SELECT Fuser_id,Fmodel,ROW_NUMBER() OVER (PARTITION BY Fuser_id ORDER BY Fid DESC) rn
		FROM `+s.t("t_llm_usage")+` WHERE Fuser_id IN (`+in+`)) x WHERE rn=1`, args...)
	if err != nil {
		return nil, err
	}
	models := map[string]string{}
	for modelRows.Next() {
		var id, model string
		if err := modelRows.Scan(&id, &model); err != nil {
			modelRows.Close()
			return nil, err
		}
		models[id] = model
	}
	modelRows.Close()
	for i := range out {
		id := out[i].UserID
		out[i].Name = names[id]
		out[i].Status = statuses[id]
		out[i].ActiveRuns = active[id]
		out[i].Sessions = sessions[id]
		out[i].Tokens24h = tokens24[id]
		out[i].Model = models[id]
	}
	return out, nil
}

var _ = strings.Join
