package api

import (
	"net/http"
	"time"
)

type loginRequest struct {
	AdminID  string `json:"admin_id"`
	Password string `json:"password"`
}

func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"product_name": s.Cfg.UI.ProductName,
		"version":      s.Cfg.UI.Version,
		"environment":  s.Cfg.Environment,
		"mode":         s.Cfg.Mode,
		"exposure":     s.Cfg.Exposure,
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.Services.Store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "database unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.Cfg.UI.Version, "time": time.Now().UTC()})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if in.AdminID == "" || in.Password == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "admin_id and password are required")
		return
	}
	p, raw, exp, err := s.Auth.Login(r.Context(), in.AdminID, in.Password, clientIP(r, s.Cfg.Security.TrustedProxies), r.UserAgent(), requestID(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	s.setSessionCookie(w, raw, exp)
	_ = p
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p != nil {
		_ = s.Auth.Logout(r.Context(), p, authMeta(r, s.Cfg))
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	elevatedUntil := p.Session.ElevatedUntil
	writeJSON(w, http.StatusOK, map[string]any{
		"admin_id":               p.Admin.AdminID,
		"display_name":           p.Admin.DisplayName,
		"role":                   p.Admin.Role,
		"csrf_token":             p.CSRFToken,
		"elevated":               p.Elevated,
		"elevated_until":         elevatedUntil,
		"environment":            s.Cfg.Environment,
		"mode":                   s.Cfg.Mode,
		"exposure":               s.Cfg.Exposure,
		"version":                s.Cfg.UI.Version,
		"require_step_up_writes": s.Cfg.Security.RequireStepUpWrites,
	})
}

type stepUpRequest struct {
	Password string `json:"password"`
}

func (s *Server) stepUp(w http.ResponseWriter, r *http.Request) {
	var in stepUpRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := s.Auth.StepUp(r.Context(), principal(r), in.Password, authMeta(r, s.Cfg)); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in changePasswordRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := s.Auth.ChangePassword(r.Context(), principal(r), in.OldPassword, in.NewPassword, authMeta(r, s.Cfg)); err != nil {
		writeServiceError(w, err)
		return
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "password changed; sign in again"})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, raw string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: s.Cfg.Security.CookieName, Value: raw, Path: "/",
		HttpOnly: true, Secure: s.Cfg.CookieSecure(), SameSite: http.SameSiteStrictMode,
		Expires: exp, MaxAge: max(1, int(time.Until(exp).Seconds())),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.Cfg.Security.CookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: s.Cfg.CookieSecure(), SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}
