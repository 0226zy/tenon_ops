package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
)

type System struct{ s *Services }

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok | warn | error
	Message string `json:"message"`
}

type Health struct {
	OK            bool       `json:"ok"`
	Version       string     `json:"version"`
	Mode          string     `json:"mode"`
	Exposure      string     `json:"exposure"`
	Environment   string     `json:"environment"`
	Listen        string     `json:"listen"`
	PublicURL     string     `json:"public_url"`
	SchemaVersion string     `json:"schema_version"`
	TenonSchema   string     `json:"tenon_schema"`
	AdminCount    int        `json:"admin_count"`
	LastUsageAt   *time.Time `json:"last_usage_at,omitempty"`
	LastEventAt   *time.Time `json:"last_event_at,omitempty"`
	Checks        []Check    `json:"checks"`
	Time          time.Time  `json:"time"`
}

func (s *System) Health(ctx context.Context) (*Health, error) {
	h := &Health{
		Version: s.s.Cfg.UI.Version, Mode: s.s.Cfg.Mode, Exposure: s.s.Cfg.Exposure,
		Environment: s.s.Cfg.Environment, Listen: s.s.Cfg.Listen, PublicURL: s.s.Cfg.PublicURL,
		TenonSchema: s.s.Cfg.DB.TenonSchema, Time: time.Now().UTC(),
	}
	ok := true
	add := func(name, status, msg string) {
		h.Checks = append(h.Checks, Check{Name: name, Status: status, Message: msg})
		if status == "error" {
			ok = false
		}
	}
	if err := s.s.Store.Ping(ctx); err != nil {
		add("database", "error", err.Error())
	} else {
		add("database", "ok", "connected")
	}
	version, err := s.s.Store.SchemaVersion(ctx)
	if err != nil {
		add("tenon_schema_version", "error", err.Error())
	} else {
		h.SchemaVersion = version
		if version == "" {
			add("tenon_schema_version", "warn", "no t_schema_migration rows")
		} else {
			add("tenon_schema_version", "ok", version)
		}
	}
	if _, err := s.s.Store.CheckTenonSchema(ctx); err != nil {
		add("t_user_contract", "error", err.Error())
	} else {
		add("t_user_contract", "ok", "compatible")
	}
	count, err := s.s.Store.AdminCount(ctx)
	if err != nil {
		add("admin", "error", err.Error())
	} else {
		h.AdminCount = count
		if count == 0 {
			add("admin", "error", "no administrator configured")
		} else {
			add("admin", "ok", fmt.Sprintf("%d administrator(s)", count))
		}
	}
	keyPath := filepath.Join(s.s.Cfg.Security.KeysDir, "master.key")
	if info, err := os.Stat(keyPath); err != nil {
		add("master_key", "error", err.Error())
	} else if info.Mode().Perm()&0o077 != 0 {
		add("master_key", "warn", fmt.Sprintf("permissions %o; expected 0600", info.Mode().Perm()))
	} else {
		add("master_key", "ok", "present, 0600")
	}
	if s.s.Cfg.Exposure == "public" {
		add("exposure", "warn", "public exposure; MFA is not implemented in this build")
	} else {
		add("exposure", "ok", "private; use VPN/IP allowlist")
	}
	if s.s.Cfg.PublicURL == "" {
		add("public_url", "warn", "public_url is empty; Origin allowlist may be incomplete")
	} else {
		add("public_url", "ok", s.s.Cfg.PublicURL)
	}
	lastUsage, _ := s.s.Store.LastUsageAt(ctx)
	lastEvent, _ := s.s.Store.LastEventAt(ctx)
	h.LastUsageAt, h.LastEventAt = lastUsage, lastEvent
	if lastUsage == nil {
		add("usage_data", "warn", "no model usage yet")
	} else {
		add("usage_data", "ok", "last at "+lastUsage.Format(time.RFC3339))
	}
	h.OK = ok
	return h, nil
}

func (s *System) Settings(ctx context.Context) ([]store.Setting, error) {
	return s.s.Store.Settings(ctx)
}

var allowedSettings = map[string]bool{
	"ui.timezone": true, "ui.refresh_default_seconds": true, "presence.default_window": true,
	"usage.cache_seconds": true, "audit.retention_days": true, "export.retention_hours": true, "export.max_rows": true,
}

func (s *System) UpdateSettings(ctx context.Context, values map[string]string, meta Meta) error {
	for k, v := range values {
		if !allowedSettings[k] {
			return BadRequest("BAD_SETTING", "unknown setting: "+k)
		}
		switch k {
		case "ui.timezone":
			if _, err := time.LoadLocation(v); err != nil {
				return BadRequest("BAD_SETTING", "invalid timezone")
			}
		case "presence.default_window":
			if _, err := parseWindow(v); err != nil {
				return BadRequest("BAD_SETTING", "invalid presence window")
			}
		default:
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return BadRequest("BAD_SETTING", "invalid numeric setting "+k)
			}
		}
	}
	for k, v := range values {
		if err := s.s.Store.PutSetting(ctx, k, v); err != nil {
			return Wrap(err, "save setting")
		}
	}
	return s.s.audit(ctx, meta, "settings.update", "settings", "", 1, "", values)
}
