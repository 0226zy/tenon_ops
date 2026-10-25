// Package store owns all SQL for tenon_ops. It is MySQL 8 specific.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/0226zy/tenon_ops/internal/config"
	mysql "github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
)

type Querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db               *sql.DB
	admin            string
	tenon            string
	timeout          time.Duration
	usageCacheColumn string
}

func Open(ctx context.Context, cfg *config.Config) (*Store, error) {
	my, err := mysql.ParseDSN(cfg.DB.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	my.ParseTime = true
	my.Loc = time.UTC
	my.Timeout = 5 * time.Second
	my.ReadTimeout = 30 * time.Second
	my.WriteTimeout = 30 * time.Second
	if my.Params == nil {
		my.Params = map[string]string{}
	}
	if _, ok := my.Params["charset"]; !ok {
		my.Params["charset"] = "utf8mb4"
	}
	db, err := sql.Open("mysql", my.FormatDSN())
	if err != nil {
		return nil, err
	}
	maxOpen := cfg.DB.MaxOpen
	if maxOpen <= 0 {
		maxOpen = 10
	}
	maxIdle := cfg.DB.MaxIdle
	if maxIdle <= 0 {
		maxIdle = 3
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	st := &Store{db: db, admin: cfg.DB.AdminSchema, tenon: cfg.DB.TenonSchema, timeout: 10 * time.Second}
	st.usageCacheColumn = st.detectUsageCacheColumn(ctx)
	return st, nil
}

// detectUsageCacheColumn is forward-compatible: current Tenon schemas do not
// expose cached prompt tokens, but a future column can be used automatically.
func (s *Store) detectUsageCacheColumn(ctx context.Context) string {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT COLUMN_NAME FROM information_schema.columns
		WHERE TABLE_SCHEMA=? AND TABLE_NAME='t_llm_usage' AND COLUMN_NAME IN ('Fcache_hit_tokens','Fcached_tokens') LIMIT 1`, s.tenon).Scan(&name)
	if err != nil {
		return ""
	}
	switch name {
	case "Fcache_hit_tokens", "Fcached_tokens":
		return name
	default:
		return ""
	}
}

func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

var identPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func quoteIdent(v string) string {
	if !identPattern.MatchString(v) {
		panic("unsafe SQL identifier: " + v)
	}
	return "`" + v + "`"
}

func (s *Store) a(table string) string { return quoteIdent(s.admin) + "." + quoteIdent(table) }
func (s *Store) t(table string) string { return quoteIdent(s.tenon) + "." + quoteIdent(table) }

func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func IsDuplicate(err error) bool {
	if err == nil {
		return false
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}

func IsNoSuchTable(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1146
	}
	return false
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	x := v.Time.UTC()
	return &x
}

func utcNow() time.Time { return time.Now().UTC() }

func Limit(limit, fallback, max int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > max {
		return max
	}
	return limit
}

// CheckTenonSchema verifies the columns tenon_ops writes/reads. It refuses to
// run if t_user has a NOT NULL column without a default that we do not know.
func (s *Store) CheckTenonSchema(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COLUMN_NAME, IS_NULLABLE, COLUMN_DEFAULT, EXTRA
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA=? AND TABLE_NAME='t_user'
		ORDER BY ORDINAL_POSITION`, s.tenon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]bool{
		"Fid": true, "Fuser_id": true, "Fname": true, "Fpwd_hash": true,
		"Fstatus": true, "Fmax_conv": true, "Fcreate_time": true, "Fmodify_time": true,
	}
	var warnings []string
	found := 0
	for rows.Next() {
		var name, nullable, extra string
		var def sql.NullString
		if err := rows.Scan(&name, &nullable, &def, &extra); err != nil {
			return nil, err
		}
		found++
		if !known[name] && nullable == "NO" && !def.Valid && !strings.Contains(extra, "auto_increment") {
			return nil, fmt.Errorf("tenon schema incompatible: t_user has unknown NOT NULL column %s", name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if found == 0 {
		return nil, fmt.Errorf("tenon schema %s has no t_user table", s.tenon)
	}
	return warnings, nil
}

func (s *Store) SchemaVersion(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT version FROM "+s.t("t_schema_migration")+" ORDER BY version DESC LIMIT 1").Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) LastUsageAt(ctx context.Context) (*time.Time, error) {
	var t sql.NullTime
	err := s.db.QueryRowContext(ctx, "SELECT MAX(Fcreate_time) FROM "+s.t("t_llm_usage")).Scan(&t)
	return timePtr(t), err
}

func (s *Store) LastEventAt(ctx context.Context) (*time.Time, error) {
	var t sql.NullTime
	err := s.db.QueryRowContext(ctx, "SELECT MAX(Fcreate_time) FROM "+s.t("t_event")).Scan(&t)
	return timePtr(t), err
}
