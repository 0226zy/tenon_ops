// Package services implements the product use cases on top of store/security.
package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/0226zy/tenon_ops/internal/auth"
	"github.com/0226zy/tenon_ops/internal/config"
	"github.com/0226zy/tenon_ops/internal/security"
	"github.com/0226zy/tenon_ops/internal/store"
)

type Services struct {
	Store     *store.Store
	Keys      *security.Keys
	Vault     *security.Vault
	Cfg       *config.Config
	Users     *Users
	Models    *Models
	Usage     *Usage
	Presence  *Presence
	Runs      *Runs
	Audit     *Audit
	Dashboard *Dashboard
	System    *System
	Auth      *auth.Service
}

func New(st *store.Store, keys *security.Keys, cfg *config.Config, authSvc *auth.Service) *Services {
	vault := security.NewVault(keys.Vault)
	s := &Services{Store: st, Keys: keys, Vault: vault, Cfg: cfg, Auth: authSvc}
	s.Users = &Users{s: s}
	s.Models = &Models{s: s}
	s.Usage = &Usage{s: s}
	s.Presence = &Presence{s: s}
	s.Runs = &Runs{s: s}
	s.Audit = &Audit{s: s}
	s.Dashboard = &Dashboard{s: s}
	s.System = &System{s: s}
	return s
}

type Meta struct {
	AdminID   string
	IP        string
	UA        string
	RequestID string
}

func (s *Services) audit(ctx context.Context, meta Meta, action, targetType, targetID string, result int, reason string, detail any) error {
	var raw json.RawMessage
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = b
	}
	return s.Store.InsertAudit(ctx, &store.AuditEntry{
		TS: time.Now().UTC(), AdminID: meta.AdminID, Action: action,
		TargetType: targetType, TargetID: targetID, Result: result,
		IP: meta.IP, UA: meta.UA, RequestID: meta.RequestID, Reason: reason, Detail: raw,
	})
}
