package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const adminCols = `Fid,Fadmin_id,Fdisplay_name,Frole,Fpwd_algo,Fpwd_params,Fpwd_salt,Fpwd_hash,Fpwd_version,Fsession_epoch,Fstatus,Ffailed_count,Flocked_until,Flast_login_at,Flast_login_ip,Fpwd_changed_at,Fcreate_time,Fmodify_time`

func scanAdmin(row interface{ Scan(...any) error }) (*Admin, error) {
	var a Admin
	var locked, last sql.NullTime
	err := row.Scan(&a.ID, &a.AdminID, &a.DisplayName, &a.Role, &a.PwdAlgo, &a.PwdParams, &a.PwdSalt, &a.PwdHash, &a.PwdVersion, &a.SessionEpoch, &a.Status, &a.FailedCount, &locked, &last, &a.LastLoginIP, &a.PwdChangedAt, &a.CreatedAt, &a.ModifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.LockedUntil = timePtr(locked)
	a.LastLoginAt = timePtr(last)
	return &a, nil
}

func (s *Store) AdminByID(ctx context.Context, adminID string) (*Admin, error) {
	return scanAdmin(s.db.QueryRowContext(ctx, `SELECT `+adminCols+` FROM `+s.a("tadm_admin")+` WHERE Fadmin_id=?`, adminID))
}

func (s *Store) AdminCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.a("tadm_admin")).Scan(&n)
	return n, err
}

func (s *Store) CreateAdmin(ctx context.Context, a *Admin) error {
	now := utcNow()
	a.CreatedAt, a.ModifiedAt = now, now
	res, err := s.db.ExecContext(ctx, `INSERT INTO `+s.a("tadm_admin")+`
		(Fadmin_id,Fdisplay_name,Frole,Fpwd_algo,Fpwd_params,Fpwd_salt,Fpwd_hash,Fpwd_version,Fsession_epoch,Fstatus,Ffailed_count,Flocked_until,Flast_login_at,Flast_login_ip,Fpwd_changed_at,Fcreate_time,Fmodify_time)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.AdminID, a.DisplayName, a.Role, a.PwdAlgo, a.PwdParams, a.PwdSalt, a.PwdHash, a.PwdVersion, a.SessionEpoch, a.Status, a.FailedCount, nullTime(a.LockedUntil), nullTime(a.LastLoginAt), a.LastLoginIP, a.PwdChangedAt, now, now)
	if IsDuplicate(err) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) RecordAdminSuccess(ctx context.Context, adminID, ip string) error {
	now := utcNow()
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_admin")+`
		SET Ffailed_count=0,Flocked_until=NULL,Flast_login_at=?,Flast_login_ip=?,Fmodify_time=?
		WHERE Fadmin_id=?`, now, truncate(ip, 64), now, adminID)
	return err
}

// RecordAdminFailure increments the persistent failure counter and locks the
// account when the threshold is reached. It returns the current count.
func (s *Store) RecordAdminFailure(ctx context.Context, adminID string, limit int, lock time.Duration) (int, *time.Time, error) {
	now := utcNow()
	var count int
	var locked sql.NullTime
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var current sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT Ffailed_count,Flocked_until FROM `+s.a("tadm_admin")+` WHERE Fadmin_id=? FOR UPDATE`, adminID).Scan(&count, &current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		count++
		locked = current
		if count >= limit {
			t := now.Add(lock)
			locked = sql.NullTime{Time: t, Valid: true}
			count = 0
		}
		_, err := tx.ExecContext(ctx, `UPDATE `+s.a("tadm_admin")+` SET Ffailed_count=?,Flocked_until=?,Fmodify_time=? WHERE Fadmin_id=?`, count, nullTime(timePtr(locked)), now, adminID)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	return count, timePtr(locked), nil
}

func (s *Store) ClearAdminFailure(ctx context.Context, adminID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_admin")+` SET Ffailed_count=0,Flocked_until=NULL,Fmodify_time=? WHERE Fadmin_id=?`, utcNow(), adminID)
	return err
}

func (s *Store) UpdateAdminVerifier(ctx context.Context, adminID string, salt, hash []byte, params, algo string) error {
	now := utcNow()
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_admin")+`
		SET Fpwd_algo=?,Fpwd_params=?,Fpwd_salt=?,Fpwd_hash=?,Fpwd_version=Fpwd_version+1,Fsession_epoch=Fsession_epoch+1,Fpwd_changed_at=?,Fmodify_time=?
		WHERE Fadmin_id=?`, algo, params, salt, hash, now, now, adminID)
	return err
}

const sessionCols = `Fid,Fsid_hash,Fadmin_id,Fexpire_time,Fidle_expire_time,Felevated_until,Fip,Fua,Frevoked,Fcreate_time,Flast_seen_at`

func scanSession(row interface{ Scan(...any) error }) (*AdminSession, error) {
	var v AdminSession
	var elevated, last sql.NullTime
	err := row.Scan(&v.ID, &v.SIDHash, &v.AdminID, &v.ExpireAt, &v.IdleExpireAt, &elevated, &v.IP, &v.UA, &v.Revoked, &v.CreatedAt, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.ElevatedUntil = timePtr(elevated)
	v.LastSeenAt = timePtr(last)
	return &v, nil
}

func (s *Store) CreateSession(ctx context.Context, v *AdminSession) error {
	v.CreatedAt = utcNow()
	res, err := s.db.ExecContext(ctx, `INSERT INTO `+s.a("tadm_admin_session")+`
		(Fsid_hash,Fadmin_id,Fexpire_time,Fidle_expire_time,Felevated_until,Fip,Fua,Frevoked,Fcreate_time,Flast_seen_at)
		VALUES(?,?,?,?,?,?,?,0,?,NULL)`,
		v.SIDHash, v.AdminID, v.ExpireAt, v.IdleExpireAt, nullTime(v.ElevatedUntil), truncate(v.IP, 64), truncate(v.UA, 255), v.CreatedAt)
	if err != nil {
		return err
	}
	v.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) SessionByHash(ctx context.Context, hash string) (*AdminSession, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM `+s.a("tadm_admin_session")+` WHERE Fsid_hash=?`, hash))
}

func (s *Store) TouchSession(ctx context.Context, id int64, idle time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_admin_session")+` SET Flast_seen_at=?,Fidle_expire_time=? WHERE Fid=?`, utcNow(), idle, id)
	return err
}

func (s *Store) SetSessionElevated(ctx context.Context, id int64, until time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_admin_session")+` SET Felevated_until=? WHERE Fid=?`, until, id)
	return err
}

func (s *Store) RevokeSession(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_admin_session")+` SET Frevoked=1 WHERE Fid=?`, id)
	return err
}

func (s *Store) RevokeAdminSessions(ctx context.Context, adminID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_admin_session")+` SET Frevoked=1 WHERE Fadmin_id=?`, adminID)
	return err
}

func (s *Store) CleanupSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+s.a("tadm_admin_session")+` WHERE Fexpire_time < DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 DAY)`)
	return err
}

func truncate(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return v[:n]
}
