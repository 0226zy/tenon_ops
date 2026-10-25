package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/0226zy/tenon_ops/migrations"
	mysql "github.com/go-sql-driver/mysql"
)

// InitSchema executes the embedded MySQL schema. The DSN may point at a
// database that does not exist yet; it is opened without a default schema.
func InitSchema(ctx context.Context, dsn, adminSchema string) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect mysql: %w", err)
	}
	script, err := migrations.InitSQL()
	if err != nil {
		return err
	}
	if adminSchema != "" && adminSchema != "tenon_admin" {
		script = strings.ReplaceAll(script, "`tenon_admin`", "`"+adminSchema+"`")
	}
	statements, err := SplitSQL(script)
	if err != nil {
		return err
	}
	for i, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("statement %d failed: %w\n%s", i+1, err, truncateSQL(stmt))
		}
	}
	return nil
}

func truncateSQL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 240 {
		return s[:240] + "..."
	}
	return s
}

// SplitSQL splits a MySQL script on semicolons outside quotes/comments.
func SplitSQL(script string) ([]string, error) {
	var out []string
	var buf strings.Builder
	var quote byte
	lineComment := false
	blockComment := false
	for i := 0; i < len(script); i++ {
		c := script[i]
		if lineComment {
			buf.WriteByte(c)
			if c == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			buf.WriteByte(c)
			if c == '*' && i+1 < len(script) && script[i+1] == '/' {
				buf.WriteByte('/')
				i++
				blockComment = false
			}
			continue
		}
		if quote != 0 {
			buf.WriteByte(c)
			if c == '\\' && quote != '`' && i+1 < len(script) {
				buf.WriteByte(script[i+1])
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '-' && i+1 < len(script) && script[i+1] == '-' {
			lineComment = true
			buf.WriteString("--")
			i++
			continue
		}
		if c == '/' && i+1 < len(script) && script[i+1] == '*' {
			blockComment = true
			buf.WriteString("/*")
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			buf.WriteByte(c)
			continue
		}
		if c == ';' {
			if s := strings.TrimSpace(buf.String()); s != "" && meaningfulStatement(s) {
				out = append(out, s)
			}
			buf.Reset()
			continue
		}
		buf.WriteByte(c)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in SQL script")
	}
	if s := strings.TrimSpace(buf.String()); s != "" && meaningfulStatement(s) {
		out = append(out, s)
	}
	return out, nil
}

// meaningfulStatement returns false for comment-only chunks (e.g. a trailing
// "-- note" after the last semicolon).
func meaningfulStatement(s string) bool {
	lineComment, blockComment := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				i++
				blockComment = false
			}
			continue
		}
		if c == '-' && i+1 < len(s) && s[i+1] == '-' {
			lineComment = true
			i++
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		return true
	}
	return false
}
