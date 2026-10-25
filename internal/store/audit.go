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

type AuditFilters struct {
	AdminID string
	Action  string
	Target  string
	Result  int
	From    *time.Time
	To      *time.Time
	Cursor  string
	Limit   int
}

func (s *Store) InsertAudit(ctx context.Context, e *AuditEntry) error {
	return s.insertAudit(ctx, s.db, e)
}

func (s *Store) InsertAuditTx(ctx context.Context, tx *sql.Tx, e *AuditEntry) error {
	return s.insertAudit(ctx, tx, e)
}

func (s *Store) insertAudit(ctx context.Context, q Querier, e *AuditEntry) error {
	if e.TS.IsZero() {
		e.TS = utcNow()
	}
	var detail any
	if len(e.Detail) > 0 {
		detail = string(e.Detail)
	}
	res, err := q.ExecContext(ctx, `INSERT INTO `+s.a("tadm_audit_log")+`
		(Fts,Fadmin_id,Faction,Ftarget_type,Ftarget_id,Fresult,Fip,Fua,Frequest_id,Freason,Fdetail)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.TS.UTC(), truncate(e.AdminID, 64), truncate(e.Action, 64), truncate(e.TargetType, 32), truncate(e.TargetID, 64), e.Result, truncate(e.IP, 64), truncate(e.UA, 255), truncate(e.RequestID, 26), truncate(e.Reason, 255), detail)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) ListAudit(ctx context.Context, f AuditFilters) ([]AuditEntry, string, error) {
	limit := Limit(f.Limit, 50, 200)
	where := []string{"1=1"}
	var args []any
	if f.AdminID != "" {
		where = append(where, "Fadmin_id=?")
		args = append(args, f.AdminID)
	}
	if f.Action != "" {
		where = append(where, "Faction=?")
		args = append(args, f.Action)
	}
	if f.Target != "" {
		where = append(where, "(Ftarget_id=? OR Ftarget_type=?)")
		args = append(args, f.Target, f.Target)
	}
	if f.Result == 0 || f.Result == 1 {
		where = append(where, "Fresult=?")
		args = append(args, f.Result)
	}
	if f.From != nil {
		where = append(where, "Fts>=?")
		args = append(args, f.From.UTC())
	}
	if f.To != nil {
		where = append(where, "Fts<?")
		args = append(args, f.To.UTC())
	}
	if f.Cursor != "" {
		id, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil {
			return nil, "", err
		}
		where = append(where, "Fid<?")
		args = append(args, id)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT Fid,Fts,Fadmin_id,Faction,Ftarget_type,Ftarget_id,Fresult,Fip,Fua,Frequest_id,Freason,Fdetail FROM `+s.a("tadm_audit_log")+`
		WHERE `+strings.Join(where, " AND ")+` ORDER BY Fid DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var detail []byte
		if err := rows.Scan(&e.ID, &e.TS, &e.AdminID, &e.Action, &e.TargetType, &e.TargetID, &e.Result, &e.IP, &e.UA, &e.RequestID, &e.Reason, &detail); err != nil {
			return nil, "", err
		}
		if len(detail) > 0 {
			e.Detail = json.RawMessage(detail)
		}
		out = append(out, e)
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

func (s *Store) AuditActions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT Faction FROM `+s.a("tadm_audit_log")+` ORDER BY Faction`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type Operation struct {
	RequestID   string
	Kind        string
	TargetID    string
	RequestHash string
	Status      int
	Result      json.RawMessage
	CreatedAt   time.Time
	ExpireAt    time.Time
}

func (s *Store) BeginOperation(ctx context.Context, op *Operation, q Querier) error {
	if q == nil {
		q = s.db
	}
	if op.CreatedAt.IsZero() {
		op.CreatedAt = utcNow()
	}
	var result any
	if len(op.Result) > 0 {
		result = string(op.Result)
	}
	_, err := q.ExecContext(ctx, `INSERT INTO `+s.a("tadm_operation")+`
		(Frequest_id,Fkind,Ftarget_id,Frequest_hash,Fstatus,Fresult,Fcreate_time,Fexpire_time)
		VALUES(?,?,?,?,?,?,?,?)`, op.RequestID, op.Kind, op.TargetID, op.RequestHash, op.Status, result, op.CreatedAt, op.ExpireAt)
	if IsDuplicate(err) {
		return ErrDuplicate
	}
	return err
}

func (s *Store) CompleteOperation(ctx context.Context, q Querier, requestID string, status int, result json.RawMessage) error {
	if q == nil {
		q = s.db
	}
	var v any
	if len(result) > 0 {
		v = string(result)
	}
	_, err := q.ExecContext(ctx, `UPDATE `+s.a("tadm_operation")+` SET Fstatus=?,Fresult=? WHERE Frequest_id=?`, status, v, requestID)
	return err
}

func (s *Store) Operation(ctx context.Context, requestID string) (*Operation, error) {
	var op Operation
	var result []byte
	err := s.db.QueryRowContext(ctx, `SELECT Frequest_id,Fkind,Ftarget_id,Frequest_hash,Fstatus,Fresult,Fcreate_time,Fexpire_time FROM `+s.a("tadm_operation")+` WHERE Frequest_id=?`, requestID).
		Scan(&op.RequestID, &op.Kind, &op.TargetID, &op.RequestHash, &op.Status, &result, &op.CreatedAt, &op.ExpireAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(result) > 0 {
		op.Result = json.RawMessage(result)
	}
	return &op, nil
}

func (s *Store) CleanupOperations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+s.a("tadm_operation")+` WHERE Fexpire_time<UTC_TIMESTAMP()`)
	return err
}

var _ = sql.ErrNoRows
