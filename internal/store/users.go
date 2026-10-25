package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type UserWithProfile struct {
	User
	Profile     *UserProfile
	HasSecret   bool
	HasProfile  bool
	SecretState int
}

const userSelect = `u.Fid,u.Fuser_id,u.Fname,u.Fpwd_hash,u.Fstatus,u.Fmax_conv,u.Fcreate_time,u.Fmodify_time,
 p.Fnote,p.Ftags,p.Fpwd_state,p.Fcreated_by,p.Flast_reveal_at,p.Freveal_count,p.Fdisabled_reason,p.Fcreate_time,p.Fmodify_time,
 s.Ftenon_user_id`

func scanUserWithProfile(row interface{ Scan(...any) error }) (*UserWithProfile, error) {
	var out UserWithProfile
	var note, createdBy, disabledReason, secretID sql.NullString
	var tagsRaw []byte
	var pwdState sql.NullInt64
	var lastReveal, pCreate, pModify sql.NullTime
	var revealCount sql.NullInt64
	err := row.Scan(
		&out.ID, &out.UserID, &out.Name, &out.PwdHash, &out.Status, &out.MaxConv, &out.CreatedAt, &out.ModifiedAt,
		&note, &tagsRaw, &pwdState, &createdBy, &lastReveal, &revealCount, &disabledReason, &pCreate, &pModify,
		&secretID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if note.Valid || pwdState.Valid {
		out.HasProfile = true
		p := &UserProfile{
			UserID: out.UserID, Note: note.String, PwdState: int(pwdState.Int64),
			CreatedBy: createdBy.String, RevealCount: int(revealCount.Int64),
			DisabledReason: disabledReason.String,
		}
		p.LastRevealAt = timePtr(lastReveal)
		p.CreatedAt = pCreate.Time
		p.ModifiedAt = pModify.Time
		if len(tagsRaw) > 0 {
			_ = json.Unmarshal(tagsRaw, &p.Tags)
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		out.Profile = p
	}
	out.HasSecret = secretID.Valid
	if out.HasSecret {
		out.SecretState = 1
	}
	return &out, nil
}

func (s *Store) UserByID(ctx context.Context, userID string) (*User, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT Fid,Fuser_id,Fname,Fpwd_hash,Fstatus,Fmax_conv,Fcreate_time,Fmodify_time FROM `+s.t("t_user")+` WHERE Fuser_id=?`, userID).
		Scan(&u.ID, &u.UserID, &u.Name, &hash, &u.Status, &u.MaxConv, &u.CreatedAt, &u.ModifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	u.PwdHash = hash
	return &u, err
}

// LockUser locks one t_user row inside a transaction.
func (s *Store) LockUser(ctx context.Context, tx *sql.Tx, userID string) (*User, error) {
	var u User
	var hash string
	err := tx.QueryRowContext(ctx, `SELECT Fid,Fuser_id,Fname,Fpwd_hash,Fstatus,Fmax_conv,Fcreate_time,Fmodify_time FROM `+s.t("t_user")+` WHERE Fuser_id=? FOR UPDATE`, userID).
		Scan(&u.ID, &u.UserID, &u.Name, &hash, &u.Status, &u.MaxConv, &u.CreatedAt, &u.ModifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	u.PwdHash = hash
	return &u, err
}

func (s *Store) UserWithProfile(ctx context.Context, userID string) (*UserWithProfile, error) {
	q := `SELECT ` + userSelect + ` FROM ` + s.t("t_user") + ` u
		LEFT JOIN ` + s.a("tadm_tenon_user") + ` p ON p.Ftenon_user_id=u.Fuser_id
		LEFT JOIN ` + s.a("tadm_user_secret") + ` s ON s.Ftenon_user_id=u.Fuser_id
		WHERE u.Fuser_id=?`
	return scanUserWithProfile(s.db.QueryRowContext(ctx, q, userID))
}

func (s *Store) ListUsers(ctx context.Context, f UserFilters) ([]UserWithProfile, string, error) {
	limit := Limit(f.Limit, 50, 100)
	var args []any
	where := []string{"1=1"}
	if f.Query != "" {
		like := "%" + f.Query + "%"
		where = append(where, "(u.Fuser_id LIKE ? OR u.Fname LIKE ?)")
		args = append(args, like, like)
	}
	if f.Status != 0 {
		where = append(where, "u.Fstatus=?")
		args = append(args, f.Status)
	}
	if f.PwdState >= 0 && f.PwdState <= 2 {
		switch f.PwdState {
		case 0:
			where = append(where, "(p.Fpwd_state=0 OR p.Fpwd_state IS NULL) AND s.Ftenon_user_id IS NULL")
		case 1:
			where = append(where, "(p.Fpwd_state=1 OR s.Ftenon_user_id IS NOT NULL)")
		case 2:
			where = append(where, "p.Fpwd_state=2")
		}
	}
	if f.Cursor != "" {
		var id int64
		if _, err := fmt.Sscan(f.Cursor, &id); err != nil {
			return nil, "", err
		}
		where = append(where, "u.Fid<?")
		args = append(args, id)
	}
	args = append(args, limit+1)
	q := `SELECT ` + userSelect + ` FROM ` + s.t("t_user") + ` u
		LEFT JOIN ` + s.a("tadm_tenon_user") + ` p ON p.Ftenon_user_id=u.Fuser_id
		LEFT JOIN ` + s.a("tadm_user_secret") + ` s ON s.Ftenon_user_id=u.Fuser_id
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY u.Fid DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []UserWithProfile{}
	for rows.Next() {
		v, err := scanUserWithProfile(rows)
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

func (s *Store) CreateUserTx(ctx context.Context, tx *sql.Tx, u *User) error {
	now := utcNow()
	res, err := tx.ExecContext(ctx, `INSERT INTO `+s.t("t_user")+`
		(Fuser_id,Fname,Fpwd_hash,Fstatus,Fmax_conv,Fcreate_time,Fmodify_time)
		VALUES(?,?,?,?,?,?,?)`, u.UserID, u.Name, u.PwdHash, u.Status, u.MaxConv, now, now)
	if IsDuplicate(err) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	u.CreatedAt, u.ModifiedAt = now, now
	return nil
}

func (s *Store) UpdateUserPasswordTx(ctx context.Context, tx *sql.Tx, userID, hash string) error {
	res, err := tx.ExecContext(ctx, `UPDATE `+s.t("t_user")+` SET Fpwd_hash=?,Fmodify_time=? WHERE Fuser_id=?`, hash, utcNow(), userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateUserStatusTx(ctx context.Context, tx *sql.Tx, userID string, status int) error {
	// The caller locks the row first. MySQL reports 0 affected rows when the
	// value is unchanged, which is still a success.
	_, err := tx.ExecContext(ctx, `UPDATE `+s.t("t_user")+` SET Fstatus=?,Fmodify_time=? WHERE Fuser_id=?`, status, utcNow(), userID)
	return err
}

func (s *Store) UpdateUserNameTx(ctx context.Context, tx *sql.Tx, userID, name string) error {
	_, err := tx.ExecContext(ctx, `UPDATE `+s.t("t_user")+` SET Fname=?,Fmodify_time=? WHERE Fuser_id=?`, name, utcNow(), userID)
	return err
}

func (s *Store) UpdateUserMaxConvTx(ctx context.Context, tx *sql.Tx, userID string, max int) error {
	_, err := tx.ExecContext(ctx, `UPDATE `+s.t("t_user")+` SET Fmax_conv=?,Fmodify_time=? WHERE Fuser_id=?`, max, utcNow(), userID)
	return err
}

func (s *Store) ExpireUserSessionsTx(ctx context.Context, tx *sql.Tx, userID string) error {
	now := utcNow()
	_, err := tx.ExecContext(ctx, `UPDATE `+s.t("t_auth_session")+` SET Fexpire_time=? WHERE Fuser_id=?`, now, userID)
	return err
}

func (s *Store) ExpireUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.t("t_auth_session")+` SET Fexpire_time=? WHERE Fuser_id=?`, utcNow(), userID)
	return err
}

func (s *Store) UserSessionCount(ctx context.Context, userID string, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.t("t_auth_session")+` WHERE Fuser_id=? AND Fexpire_time>?`, userID, now).Scan(&n)
	return n, err
}

func (s *Store) Profile(ctx context.Context, userID string) (*UserProfile, error) {
	var p UserProfile
	var tags []byte
	var last, created, modified sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT Ftenon_user_id,Fnote,Ftags,Fpwd_state,Fcreated_by,Flast_reveal_at,Freveal_count,Fdisabled_reason,Fcreate_time,Fmodify_time FROM `+s.a("tadm_tenon_user")+` WHERE Ftenon_user_id=?`, userID).
		Scan(&p.UserID, &p.Note, &tags, &p.PwdState, &p.CreatedBy, &last, &p.RevealCount, &p.DisabledReason, &created, &modified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.LastRevealAt = timePtr(last)
	p.CreatedAt = created.Time
	p.ModifiedAt = modified.Time
	if len(tags) > 0 {
		_ = json.Unmarshal(tags, &p.Tags)
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return &p, nil
}

func (s *Store) UpsertProfileTx(ctx context.Context, tx *sql.Tx, p *UserProfile) error {
	raw, _ := json.Marshal(p.Tags)
	now := utcNow()
	_, err := tx.ExecContext(ctx, `INSERT INTO `+s.a("tadm_tenon_user")+`
		(Ftenon_user_id,Fnote,Ftags,Fpwd_state,Fcreated_by,Fdisabled_reason,Fcreate_time,Fmodify_time)
		VALUES(?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE Fnote=VALUES(Fnote),Ftags=VALUES(Ftags),Fpwd_state=VALUES(Fpwd_state),Fdisabled_reason=VALUES(Fdisabled_reason),Fmodify_time=VALUES(Fmodify_time)`,
		p.UserID, p.Note, raw, p.PwdState, p.CreatedBy, p.DisabledReason, now, now)
	return err
}

func (s *Store) UpdateProfileTx(ctx context.Context, tx *sql.Tx, userID, note string, tags []string) error {
	raw, _ := json.Marshal(tags)
	_, err := tx.ExecContext(ctx, `UPDATE `+s.a("tadm_tenon_user")+` SET Fnote=?,Ftags=?,Fmodify_time=? WHERE Ftenon_user_id=?`, note, raw, utcNow(), userID)
	return err
}

func (s *Store) MarkReveal(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE `+s.a("tadm_tenon_user")+` SET Flast_reveal_at=?,Freveal_count=Freveal_count+1,Fmodify_time=? WHERE Ftenon_user_id=?`, utcNow(), utcNow(), userID)
	return err
}

func (s *Store) Secret(ctx context.Context, userID string) (*Secret, error) {
	var v Secret
	var rotated, delivered sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT Ftenon_user_id,Fkey_version,Fnonce,Fciphertext,Fcreated_by,Fcreate_time,Frotated_at,Fdelivered_at FROM `+s.a("tadm_user_secret")+` WHERE Ftenon_user_id=?`, userID).
		Scan(&v.UserID, &v.KeyVersion, &v.Nonce, &v.Ciphertext, &v.CreatedBy, &v.CreatedAt, &rotated, &delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.RotatedAt = timePtr(rotated)
	v.DeliveredAt = timePtr(delivered)
	return &v, nil
}

func (s *Store) PutSecretTx(ctx context.Context, tx *sql.Tx, v *Secret) error {
	now := utcNow()
	_, err := tx.ExecContext(ctx, `INSERT INTO `+s.a("tadm_user_secret")+`
		(Ftenon_user_id,Fkey_version,Fnonce,Fciphertext,Fcreated_by,Fcreate_time,Frotated_at,Fdelivered_at)
		VALUES(?,?,?,?,?,?,NULL,NULL)
		ON DUPLICATE KEY UPDATE Fkey_version=VALUES(Fkey_version),Fnonce=VALUES(Fnonce),Fciphertext=VALUES(Fciphertext),Fcreated_by=VALUES(Fcreated_by),Frotated_at=VALUES(Fcreate_time)`,
		v.UserID, v.KeyVersion, v.Nonce, v.Ciphertext, v.CreatedBy, now)
	return err
}

func (s *Store) DeleteSecret(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+s.a("tadm_user_secret")+` WHERE Ftenon_user_id=?`, userID)
	return err
}

func (s *Store) UserActivity(ctx context.Context, userID string) (lastSeen *time.Time, sessions int, activeRuns int, convs int, tokens30d int64, err error) {
	var seen sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT
		(SELECT MAX(Flast_active_time) FROM `+s.t("t_conversation")+` WHERE Fuser_id=? AND Flstate=1),
		(SELECT COUNT(*) FROM `+s.t("t_auth_session")+` WHERE Fuser_id=? AND Fexpire_time>UTC_TIMESTAMP()),
		(SELECT COUNT(*) FROM `+s.t("t_run")+` WHERE Fuser_id=? AND Factive=1),
		(SELECT COUNT(*) FROM `+s.t("t_conversation")+` WHERE Fuser_id=? AND Flstate=1),
		(SELECT COALESCE(SUM(Fprompt_tokens+Fcompl_tokens),0) FROM `+s.t("t_llm_usage")+` WHERE Fuser_id=? AND Fcreate_time>=DATE_SUB(UTC_TIMESTAMP(), INTERVAL 30 DAY))`,
		userID, userID, userID, userID, userID).Scan(&seen, &sessions, &activeRuns, &convs, &tokens30d)
	return timePtr(seen), sessions, activeRuns, convs, tokens30d, err
}

func (s *Store) UserConversations(ctx context.Context, userID string, limit int) ([]ConversationBrief, error) {
	limit = Limit(limit, 20, 100)
	rows, err := s.db.QueryContext(ctx, `SELECT Fconv_id,Ftitle,Fmodel,Flstate,Flast_active_time,Fcreate_time FROM `+s.t("t_conversation")+` WHERE Fuser_id=? AND Flstate=1 ORDER BY Flast_active_time DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConversationBrief{}
	for rows.Next() {
		var v ConversationBrief
		if err := rows.Scan(&v.ConvID, &v.Title, &v.Model, &v.State, &v.LastActiveAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) CountUsers(ctx context.Context) (total, disabled, new7d int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(Fstatus=2),0),COALESCE(SUM(Fcreate_time>=DATE_SUB(UTC_TIMESTAMP(), INTERVAL 7 DAY)),0) FROM `+s.t("t_user")).Scan(&total, &disabled, &new7d)
	return
}

// Historical users are those with no managed secret. Used by the sync hint.
func (s *Store) UnmanagedUserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.t("t_user")+` u LEFT JOIN `+s.a("tadm_user_secret")+` s ON s.Ftenon_user_id=u.Fuser_id WHERE s.Ftenon_user_id IS NULL`).Scan(&n)
	return n, err
}
