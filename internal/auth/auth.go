// Package auth implements the single administrator login, signed sessions,
// step-up authentication and the server-held pepper policy.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/0226zy/tenon_ops/internal/config"
	"github.com/0226zy/tenon_ops/internal/security"
	"github.com/0226zy/tenon_ops/internal/store"
)

const passwordAlgo = "argon2id-pepper-v1"

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrInvalidLogin    = errors.New("invalid username or password")
	ErrMFARequired     = errors.New("mfa required")
	ErrForbidden       = errors.New("forbidden")
)

type LockedError struct{ RetryAfter time.Duration }

func (e *LockedError) Error() string { return "too many login failures" }

type Principal struct {
	Admin     *store.Admin
	Session   *store.AdminSession
	SID       string
	Token     security.SessionToken
	Elevated  bool
	CSRFToken string
}

type AuditMeta struct {
	AdminID   string
	IP        string
	UA        string
	RequestID string
}

type Service struct {
	st          *store.Store
	keys        *security.Keys
	vault       *security.Vault
	cfg         *config.Config
	dummySalt   []byte
	dummyHash   []byte
	dummyParams string

	mu       sync.Mutex
	failures map[string]*window
}

type window struct {
	count int
	first time.Time
	until time.Time
}

func New(st *store.Store, keys *security.Keys, cfg *config.Config) *Service {
	s := &Service{
		st: st, keys: keys, vault: security.NewVault(keys.Vault), cfg: cfg,
		failures: map[string]*window{},
	}
	s.dummySalt, s.dummyHash, s.dummyParams, _ = security.HashPassword("tenon-ops-dummy-password", keys.Pepper, security.DefaultPasswordParams)
	return s
}

func (s *Service) Vault() *security.Vault { return s.vault }

func (s *Service) Login(ctx context.Context, adminID, password, ip, ua, requestID string) (*Principal, string, time.Time, error) {
	adminID = strings.TrimSpace(adminID)
	key := adminID + "|" + ip
	now := time.Now().UTC()
	s.mu.Lock()
	w := s.failures[key]
	if w != nil && now.Before(w.until) {
		retry := time.Until(w.until)
		s.mu.Unlock()
		return nil, "", time.Time{}, &LockedError{RetryAfter: retry}
	}
	s.mu.Unlock()

	a, err := s.st.AdminByID(ctx, adminID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, "", time.Time{}, err
	}
	if a != nil && a.LockedUntil != nil && now.Before(*a.LockedUntil) {
		return nil, "", time.Time{}, &LockedError{RetryAfter: time.Until(*a.LockedUntil)}
	}
	salt, hash, params := s.dummySalt, s.dummyHash, s.dummyParams
	if a != nil {
		salt, hash, params = a.PwdSalt, a.PwdHash, a.PwdParams
	}
	ok := security.VerifyPassword(password, s.keys.Pepper, salt, hash, params)
	if a != nil && a.PwdAlgo != passwordAlgo {
		ok = false
	}
	if a == nil || a.Status != 1 || !ok {
		if a != nil {
			_, _, _ = s.st.RecordAdminFailure(ctx, a.AdminID, s.cfg.Security.LoginFailLimit, s.cfg.Security.LoginLock.Duration)
		}
		s.mu.Lock()
		if w == nil || now.Sub(w.first) > s.cfg.Security.LoginLock.Duration {
			w = &window{first: now}
			s.failures[key] = w
		}
		w.count++
		if w.count >= s.cfg.Security.LoginFailLimit {
			w.until = now.Add(s.cfg.Security.LoginLock.Duration)
		}
		s.mu.Unlock()
		meta := AuditMeta{IP: ip, UA: ua, RequestID: requestID}
		if a != nil {
			meta.AdminID = a.AdminID
		}
		_ = s.audit(ctx, meta, "auth.login.failure", "admin", adminID, 0, "", map[string]any{"reason": "invalid_credentials"})
		return nil, "", time.Time{}, ErrInvalidLogin
	}
	_ = s.st.RecordAdminSuccess(ctx, a.AdminID, ip)
	s.mu.Lock()
	delete(s.failures, key)
	s.mu.Unlock()

	sid, err := security.NewSessionID()
	if err != nil {
		return nil, "", time.Time{}, err
	}
	expire := now.Add(s.cfg.Security.SessionTTL.Duration)
	token := security.SessionToken{AdminID: a.AdminID, SID: sid, IssuedAt: now.Unix(), ExpireAt: expire.Unix(), Epoch: a.SessionEpoch}
	raw, err := security.SignSession(s.keys.Session, token)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	sess := &store.AdminSession{
		SIDHash: security.SHA256Hex(sid), AdminID: a.AdminID,
		ExpireAt: expire, IdleExpireAt: now.Add(s.cfg.Security.IdleTTL.Duration),
		IP: ip, UA: ua,
	}
	if err := s.st.CreateSession(ctx, sess); err != nil {
		return nil, "", time.Time{}, err
	}
	_ = s.audit(ctx, AuditMeta{AdminID: a.AdminID, IP: ip, UA: ua, RequestID: requestID}, "auth.login.success", "admin", a.AdminID, 1, "", nil)
	return &Principal{Admin: a, Session: sess, SID: sid, Token: token, CSRFToken: security.CSRFToken(s.keys.Session, sid)}, raw, expire, nil
}

func (s *Service) Authenticate(r *http.Request) (*Principal, error) {
	c, err := r.Cookie(s.cfg.Security.CookieName)
	if err != nil || c.Value == "" {
		return nil, ErrUnauthenticated
	}
	token, err := security.ParseSession(s.keys.Session, c.Value)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	sess, err := s.st.SessionByHash(r.Context(), security.SHA256Hex(token.SID))
	if err != nil || sess.Revoked == 1 || sess.AdminID != token.AdminID || time.Now().UTC().After(sess.ExpireAt) || time.Now().UTC().After(sess.IdleExpireAt) {
		return nil, ErrUnauthenticated
	}
	admin, err := s.st.AdminByID(r.Context(), token.AdminID)
	if err != nil || admin.Status != 1 {
		return nil, ErrUnauthenticated
	}
	if admin.SessionEpoch != token.Epoch {
		return nil, ErrUnauthenticated
	}
	now := time.Now().UTC()
	if sess.LastSeenAt == nil || now.Sub(*sess.LastSeenAt) > time.Minute {
		idle := now.Add(s.cfg.Security.IdleTTL.Duration)
		if idle.After(sess.ExpireAt) {
			idle = sess.ExpireAt
		}
		_ = s.st.TouchSession(r.Context(), sess.ID, idle)
		sess.IdleExpireAt = idle
	}
	p := &Principal{Admin: admin, Session: sess, SID: token.SID, Token: token, CSRFToken: security.CSRFToken(s.keys.Session, token.SID)}
	if sess.ElevatedUntil != nil && now.Before(*sess.ElevatedUntil) {
		p.Elevated = true
	}
	return p, nil
}

func (s *Service) Logout(ctx context.Context, p *Principal, meta AuditMeta) error {
	if p == nil {
		return nil
	}
	_ = s.st.RevokeSession(ctx, p.Session.ID)
	_ = s.audit(ctx, meta, "auth.logout", "admin", p.Admin.AdminID, 1, "", nil)
	return nil
}

func (s *Service) StepUp(ctx context.Context, p *Principal, password string, meta AuditMeta) error {
	if p == nil {
		return ErrUnauthenticated
	}
	if p.Admin.PwdAlgo != passwordAlgo || !security.VerifyPassword(password, s.keys.Pepper, p.Admin.PwdSalt, p.Admin.PwdHash, p.Admin.PwdParams) {
		meta.AdminID = p.Admin.AdminID
		s.audit(ctx, meta, "auth.step_up.failure", "admin", p.Admin.AdminID, 0, "", nil)
		return ErrInvalidLogin
	}
	until := time.Now().UTC().Add(s.cfg.Security.StepUpTTL.Duration)
	if err := s.st.SetSessionElevated(ctx, p.Session.ID, until); err != nil {
		return err
	}
	p.Elevated = true
	p.Session.ElevatedUntil = &until
	meta.AdminID = p.Admin.AdminID
	return s.audit(ctx, meta, "auth.step_up", "admin", p.Admin.AdminID, 1, "", nil)
}

func (s *Service) ChangePassword(ctx context.Context, p *Principal, oldPassword, newPassword string, meta AuditMeta) error {
	if p == nil {
		return ErrUnauthenticated
	}
	if p.Admin.PwdAlgo != passwordAlgo || !security.VerifyPassword(oldPassword, s.keys.Pepper, p.Admin.PwdSalt, p.Admin.PwdHash, p.Admin.PwdParams) {
		return ErrInvalidLogin
	}
	if len(newPassword) < 12 || len(newPassword) > 128 {
		return fmt.Errorf("password must be 12-128 bytes")
	}
	salt, hash, params, err := security.HashPassword(newPassword, s.keys.Pepper, security.DefaultPasswordParams)
	if err != nil {
		return err
	}
	if err := s.st.UpdateAdminVerifier(ctx, p.Admin.AdminID, salt, hash, params, "argon2id-pepper-v1"); err != nil {
		return err
	}
	_ = s.st.RevokeAdminSessions(ctx, p.Admin.AdminID)
	meta.AdminID = p.Admin.AdminID
	return s.audit(ctx, meta, "auth.password.change", "admin", p.Admin.AdminID, 1, "", nil)
}

func (s *Service) GuestLoginAllowed() bool { return false }

func (s *Service) audit(ctx context.Context, meta AuditMeta, action, targetType, targetID string, result int, reason string, detail map[string]any) error {
	var raw json.RawMessage
	if detail != nil {
		b, _ := json.Marshal(detail)
		raw = b
	}
	id := meta.AdminID
	if id == "" {
		id = currentAdmin(ctx)
	}
	return s.st.InsertAudit(ctx, &store.AuditEntry{
		TS: time.Now().UTC(), AdminID: id, Action: action,
		TargetType: targetType, TargetID: targetID, Result: result,
		IP: meta.IP, UA: meta.UA, RequestID: meta.RequestID, Reason: reason, Detail: raw,
	})
}

func currentAdmin(ctx context.Context) string {
	if v, ok := ctx.Value(adminIDKey{}).(string); ok {
		return v
	}
	return ""
}

type adminIDKey struct{}

func WithAdminID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, adminIDKey{}, id)
}

// VerifyCSRF checks the per-session CSRF token.
func (s *Service) VerifyCSRF(sid, token string) bool {
	return security.VerifyCSRF(s.keys.Session, sid, token)
}

// CSRFToken returns the token the frontend must echo on writes.
func (s *Service) CSRFToken(sid string) string {
	return security.CSRFToken(s.keys.Session, sid)
}
