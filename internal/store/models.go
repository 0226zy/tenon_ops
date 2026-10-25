package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const modelCols = `Fmodel_id,Fdisplay_name,Fprovider,Fupstream_model,Fbase_url,Fapi_key_env,Ftags,Fcontext_window,Fmax_output,Fenabled,Fis_default,Fsource,Fnotes,Fsynced_at,Fcreate_time,Fmodify_time`

func scanModel(row interface{ Scan(...any) error }) (*Model, error) {
	var m Model
	var tags []byte
	var synced, created, modified sql.NullTime
	var enabled, isDefault int
	err := row.Scan(&m.ModelID, &m.DisplayName, &m.Provider, &m.UpstreamModel, &m.BaseURL, &m.APIKeyEnv, &tags, &m.ContextWindow, &m.MaxOutput, &enabled, &isDefault, &m.Source, &m.Notes, &synced, &created, &modified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Enabled, m.IsDefault = enabled == 1, isDefault == 1
	m.SyncedAt = timePtr(synced)
	m.CreatedAt, m.ModifiedAt = created.Time, modified.Time
	if len(tags) > 0 {
		_ = json.Unmarshal(tags, &m.Tags)
	}
	if m.Tags == nil {
		m.Tags = []string{}
	}
	return &m, nil
}

func (s *Store) ListModels(ctx context.Context) ([]Model, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+modelCols+` FROM `+s.a("tadm_model")+` ORDER BY Fis_default DESC,Fmodel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Model{}
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *Store) ModelByID(ctx context.Context, id string) (*Model, error) {
	return scanModel(s.db.QueryRowContext(ctx, `SELECT `+modelCols+` FROM `+s.a("tadm_model")+` WHERE Fmodel_id=?`, id))
}

func (s *Store) UpsertModelTx(ctx context.Context, tx *sql.Tx, m *Model) error {
	tags, _ := json.Marshal(m.Tags)
	now := utcNow()
	_, err := tx.ExecContext(ctx, `INSERT INTO `+s.a("tadm_model")+`
		(Fmodel_id,Fdisplay_name,Fprovider,Fupstream_model,Fbase_url,Fapi_key_env,Ftags,Fcontext_window,Fmax_output,Fenabled,Fis_default,Fsource,Fnotes,Fsynced_at,Fcreate_time,Fmodify_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE Fdisplay_name=VALUES(Fdisplay_name),Fprovider=VALUES(Fprovider),Fupstream_model=VALUES(Fupstream_model),Fbase_url=VALUES(Fbase_url),Fapi_key_env=VALUES(Fapi_key_env),Ftags=VALUES(Ftags),Fcontext_window=VALUES(Fcontext_window),Fmax_output=VALUES(Fmax_output),Fenabled=VALUES(Fenabled),Fis_default=VALUES(Fis_default),Fsource=VALUES(Fsource),Fnotes=VALUES(Fnotes),Fsynced_at=VALUES(Fsynced_at),Fmodify_time=VALUES(Fmodify_time)`,
		m.ModelID, m.DisplayName, m.Provider, m.UpstreamModel, m.BaseURL, m.APIKeyEnv, tags, m.ContextWindow, m.MaxOutput, boolInt(m.Enabled), boolInt(m.IsDefault), m.Source, m.Notes, nullTime(m.SyncedAt), now, now)
	return err
}

func (s *Store) DisableModelsTx(ctx context.Context, tx *sql.Tx, keep []string) error {
	q := `UPDATE ` + s.a("tadm_model") + ` SET Fenabled=0,Fmodify_time=? WHERE Fenabled=1`
	args := []any{utcNow()}
	if len(keep) > 0 {
		q += ` AND Fmodel_id NOT IN (` + placeholders(len(keep)) + `)`
		for _, v := range keep {
			args = append(args, v)
		}
	}
	_, err := tx.ExecContext(ctx, q, args...)
	return err
}

func (s *Store) UpdateModelMeta(ctx context.Context, id, displayName, provider, notes string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_model")+` SET Fdisplay_name=?,Fprovider=?,Fnotes=?,Fenabled=?,Fmodify_time=? WHERE Fmodel_id=?`,
		displayName, provider, notes, boolInt(enabled), utcNow(), id)
	return err
}

func (s *Store) AddPrice(ctx context.Context, p *Price) error {
	now := utcNow()
	res, err := s.db.ExecContext(ctx, `INSERT INTO `+s.a("tadm_model_price")+`
		(Fmodel_id,Feffective_from,Finput_per_mtok,Foutput_per_mtok,Fcurrency,Fcreated_by,Fcreate_time)
		VALUES(?,?,?,?,?,?,?)`, p.ModelID, p.EffectiveFrom.UTC(), p.InputPerMTok, p.OutputPerMTok, p.Currency, p.CreatedBy, now)
	if IsDuplicate(err) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	p.CreatedAt = now
	return nil
}

func (s *Store) Prices(ctx context.Context, modelID string) ([]Price, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT Fid,Fmodel_id,Feffective_from,Finput_per_mtok,Foutput_per_mtok,Fcurrency,Fcreated_by,Fcreate_time FROM `+s.a("tadm_model_price")+` WHERE Fmodel_id=? ORDER BY Feffective_from DESC`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Price{}
	for rows.Next() {
		var p Price
		if err := rows.Scan(&p.ID, &p.ModelID, &p.EffectiveFrom, &p.InputPerMTok, &p.OutputPerMTok, &p.Currency, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) LatestPriceMap(ctx context.Context) (map[string]Price, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.Fid,p.Fmodel_id,p.Feffective_from,p.Finput_per_mtok,p.Foutput_per_mtok,p.Fcurrency,p.Fcreated_by,p.Fcreate_time
		FROM `+s.a("tadm_model_price")+` p
		INNER JOIN (SELECT Fmodel_id,MAX(Feffective_from) mx FROM `+s.a("tadm_model_price")+` GROUP BY Fmodel_id) x
		ON x.Fmodel_id=p.Fmodel_id AND x.mx=p.Feffective_from`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Price{}
	for rows.Next() {
		var p Price
		if err := rows.Scan(&p.ID, &p.ModelID, &p.EffectiveFrom, &p.InputPerMTok, &p.OutputPerMTok, &p.Currency, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		out[p.ModelID] = p
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	s := "?"
	for i := 1; i < n; i++ {
		s += ",?"
	}
	return s
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

var _ = time.Now
