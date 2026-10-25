package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Setting struct {
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	ModifiedAt time.Time `json:"modified_at"`
}

func (s *Store) Settings(ctx context.Context) ([]Setting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT Fkey,Fvalue,Fmodify_time FROM `+s.a("tadm_setting")+` ORDER BY Fkey`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Setting{}
	for rows.Next() {
		var v Setting
		if err := rows.Scan(&v.Key, &v.Value, &v.ModifiedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT Fvalue FROM `+s.a("tadm_setting")+` WHERE Fkey=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) PutSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO `+s.a("tadm_setting")+` (Fkey,Fvalue,Fmodify_time) VALUES(?,?,?)
		ON DUPLICATE KEY UPDATE Fvalue=VALUES(Fvalue),Fmodify_time=VALUES(Fmodify_time)`, key, value, utcNow())
	return err
}
