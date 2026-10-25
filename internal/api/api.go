// Package api exposes the tenon_ops HTTP API and the embedded web application.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0226zy/tenon_ops/internal/auth"
	"github.com/0226zy/tenon_ops/internal/config"
	"github.com/0226zy/tenon_ops/internal/idgen"
	"github.com/0226zy/tenon_ops/internal/services"
	"github.com/0226zy/tenon_ops/internal/store"
	"github.com/0226zy/tenon_ops/internal/web"
)

type Server struct {
	Cfg      *config.Config
	Auth     *auth.Service
	Services *services.Services
	mux      *http.ServeMux

	revealMu sync.Mutex
	reveals  map[string][]time.Time
}

type principalKey struct{}
type requestIDKey struct{}

func New(cfg *config.Config, a *auth.Service, svc *services.Services) http.Handler {
	s := &Server{Cfg: cfg, Auth: a, Services: svc, mux: http.NewServeMux(), reveals: map[string][]time.Time{}}
	s.routes()
	return s.middleware(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /api/ops/v1/public/config", s.publicConfig)
	s.mux.HandleFunc("POST /api/ops/v1/auth/login", s.login)
	s.mux.Handle("POST /api/ops/v1/auth/logout", s.secure(http.HandlerFunc(s.logout)))
	s.mux.Handle("GET /api/ops/v1/auth/me", s.secure(http.HandlerFunc(s.me)))
	s.mux.Handle("POST /api/ops/v1/auth/step-up", s.secure(http.HandlerFunc(s.stepUp)))
	s.mux.Handle("POST /api/ops/v1/auth/password", s.secure(http.HandlerFunc(s.changePassword)))
	s.mux.Handle("GET /api/ops/v1/dashboard/summary", s.secure(http.HandlerFunc(s.dashboard)))
	s.mux.Handle("GET /api/ops/v1/users", s.secure(http.HandlerFunc(s.listUsers)))
	s.mux.Handle("POST /api/ops/v1/users", s.secure(http.HandlerFunc(s.createUser)))
	s.mux.Handle("GET /api/ops/v1/users/{id}", s.secure(http.HandlerFunc(s.getUser)))
	s.mux.Handle("PATCH /api/ops/v1/users/{id}", s.secure(http.HandlerFunc(s.patchUser)))
	s.mux.Handle("POST /api/ops/v1/users/{id}/reset-password", s.secure(http.HandlerFunc(s.resetPassword)))
	s.mux.Handle("POST /api/ops/v1/users/{id}/disable", s.secure(http.HandlerFunc(s.disableUser)))
	s.mux.Handle("POST /api/ops/v1/users/{id}/enable", s.secure(http.HandlerFunc(s.enableUser)))
	s.mux.Handle("POST /api/ops/v1/users/{id}/reveal-password", s.secure(http.HandlerFunc(s.revealPassword)))
	s.mux.Handle("POST /api/ops/v1/users/{id}/sessions/revoke", s.secure(http.HandlerFunc(s.revokeSessions)))
	s.mux.Handle("GET /api/ops/v1/users/{id}/conversations", s.secure(http.HandlerFunc(s.userConversations)))
	s.mux.Handle("GET /api/ops/v1/users/{id}/runs", s.secure(http.HandlerFunc(s.userRuns)))
	s.mux.Handle("GET /api/ops/v1/users/{id}/usage", s.secure(http.HandlerFunc(s.userUsage)))
	s.mux.Handle("GET /api/ops/v1/models", s.secure(http.HandlerFunc(s.listModels)))
	s.mux.Handle("POST /api/ops/v1/models/import", s.secure(http.HandlerFunc(s.importModels)))
	s.mux.Handle("GET /api/ops/v1/models/{id}", s.secure(http.HandlerFunc(s.getModel)))
	s.mux.Handle("PATCH /api/ops/v1/models/{id}", s.secure(http.HandlerFunc(s.patchModel)))
	s.mux.Handle("GET /api/ops/v1/models/{id}/prices", s.secure(http.HandlerFunc(s.modelPrices)))
	s.mux.Handle("POST /api/ops/v1/models/{id}/prices", s.secure(http.HandlerFunc(s.addModelPrice)))
	s.mux.Handle("GET /api/ops/v1/models/{id}/usage", s.secure(http.HandlerFunc(s.modelUsage)))
	s.mux.Handle("GET /api/ops/v1/usage/overview", s.secure(http.HandlerFunc(s.usageOverview)))
	s.mux.Handle("GET /api/ops/v1/usage/export", s.secure(http.HandlerFunc(s.usageExport)))
	s.mux.Handle("GET /api/ops/v1/presence", s.secure(http.HandlerFunc(s.presence)))
	s.mux.Handle("GET /api/ops/v1/runs", s.secure(http.HandlerFunc(s.listRuns)))
	s.mux.Handle("GET /api/ops/v1/runs/{id}", s.secure(http.HandlerFunc(s.getRun)))
	s.mux.Handle("GET /api/ops/v1/runs/{id}/events", s.secure(http.HandlerFunc(s.runEvents)))
	s.mux.Handle("GET /api/ops/v1/runs/{id}/messages", s.secure(http.HandlerFunc(s.runMessages)))
	s.mux.Handle("GET /api/ops/v1/runs/{id}/usage", s.secure(http.HandlerFunc(s.runUsage)))
	s.mux.Handle("GET /api/ops/v1/audit", s.secure(http.HandlerFunc(s.listAudit)))
	s.mux.Handle("GET /api/ops/v1/audit/actions", s.secure(http.HandlerFunc(s.auditActions)))
	s.mux.Handle("GET /api/ops/v1/audit/export", s.secure(http.HandlerFunc(s.auditExport)))
	s.mux.Handle("GET /api/ops/v1/system/health", s.secure(http.HandlerFunc(s.systemHealth)))
	s.mux.Handle("GET /api/ops/v1/system/settings", s.secure(http.HandlerFunc(s.getSettings)))
	s.mux.Handle("PATCH /api/ops/v1/system/settings", s.secure(http.HandlerFunc(s.updateSettings)))
	s.mux.Handle("/", web.Handler())
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if reqID == "" || len(reqID) > 64 {
			reqID = idgen.New()
		}
		w.Header().Set("X-Request-ID", reqID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "publickey-credentials-get=(self), publickey-credentials-create=(self)")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if s.Cfg.Exposure == "public" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		rw := &statusRecorder{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in request", "request_id", reqID, "path", r.URL.Path, "panic", rec)
				writeError(rw, http.StatusInternalServerError, "INTERNAL", "internal error")
			}
			slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", time.Since(start).Milliseconds(), "request_id", reqID)
		}()
		if r.URL.Path != "/healthz" && len(s.Cfg.Security.AllowedHosts) > 0 {
			host := r.Host
			ok := false
			for _, h := range s.Cfg.Security.AllowedHosts {
				if strings.EqualFold(host, h) {
					ok = true
					break
				}
			}
			if !ok {
				writeError(w, http.StatusBadRequest, "BAD_HOST", "host is not allowed")
				return
			}
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, reqID)
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Auth.Authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.checkCSRF(r, p) {
				writeError(w, http.StatusForbidden, "CSRF_REJECTED", "request origin or token is not allowed")
				return
			}
		}
		ctx := context.WithValue(r.Context(), principalKey{}, p)
		ctx = auth.WithAdminID(ctx, p.Admin.AdminID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) checkCSRF(r *http.Request, p *auth.Principal) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		ok := false
		for _, v := range s.Cfg.Security.AllowedOrigins {
			if strings.EqualFold(origin, v) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" || !s.Auth.VerifyCSRF(p.SID, token) {
		return false
	}
	return true
}

func principal(r *http.Request) *auth.Principal {
	p, _ := r.Context().Value(principalKey{}).(*auth.Principal)
	return p
}

func requestID(r *http.Request) string {
	v, _ := r.Context().Value(requestIDKey{}).(string)
	return v
}

func (s *Server) meta(r *http.Request) services.Meta {
	p := principal(r)
	adminID := ""
	if p != nil {
		adminID = p.Admin.AdminID
	}
	return services.Meta{AdminID: adminID, IP: clientIP(r, s.Cfg.Security.TrustedProxies), UA: r.UserAgent(), RequestID: requestID(r)}
}

func clientIP(r *http.Request, trusted []string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !trustedIP(host, trusted) {
		return host
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if ip != "" && net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	return host
}

func trustedIP(ip string, trusted []string) bool {
	for _, v := range trusted {
		if ip == v {
			return true
		}
	}
	return false
}

func (s *Server) requireStepUp(p *auth.Principal) bool {
	if p == nil {
		return false
	}
	if p.Elevated {
		return true
	}
	if s.Cfg.Security.RequireStepUpWrites {
		return false
	}
	// Default: reveal/export handlers explicitly require step-up; other writes
	// remain available unless the operator enables require_step_up_writes.
	return true
}

func (s *Server) checkElevated(w http.ResponseWriter, r *http.Request) bool {
	p := principal(r)
	if p == nil || !p.Elevated {
		writeError(w, http.StatusForbidden, "STEP_UP_REQUIRED", "re-authentication required")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeServiceError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	var se *services.Error
	var le *auth.LockedError
	switch {
	case errors.As(err, &se):
		if se.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(se.RetryAfter.Seconds())+1))
		}
		writeError(w, se.Status, se.Code, se.Message)
	case errors.As(err, &le):
		w.Header().Set("Retry-After", strconv.Itoa(int(le.RetryAfter.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many login failures")
	case errors.Is(err, auth.ErrInvalidLogin):
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid username or password")
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, http.StatusConflict, "CONFLICT", "resource already exists")
	default:
		slog.Error("request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return services.BadRequest("BAD_REQUEST", "invalid request body")
	}
	return nil
}

func intQuery(r *http.Request, name string, fallback, max int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > max {
		return fallback
	}
	return n
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

var _ = fmt.Sprintf

func authMeta(r *http.Request, cfg *config.Config) auth.AuditMeta {
	p := principal(r)
	adminID := ""
	if p != nil {
		adminID = p.Admin.AdminID
	}
	return auth.AuditMeta{AdminID: adminID, IP: clientIP(r, cfg.Security.TrustedProxies), UA: r.UserAgent(), RequestID: requestID(r)}
}

// allowReveal limits password reveals to 30 per administrator per hour.
// The limiter is in-process; restarting the service resets it.
func (s *Server) allowReveal(adminID string) bool {
	s.revealMu.Lock()
	defer s.revealMu.Unlock()
	now := time.Now().UTC()
	cutoff := now.Add(-time.Hour)
	list := s.reveals[adminID]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 30 {
		s.reveals[adminID] = kept
		return false
	}
	s.reveals[adminID] = append(kept, now)
	return true
}
