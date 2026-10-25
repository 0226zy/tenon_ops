package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/0226zy/tenon_ops/internal/services"
	"github.com/0226zy/tenon_ops/internal/store"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.UserFilters{
		Query: q.Get("q"), Limit: intQuery(r, "limit", 50, 100),
		Cursor: q.Get("cursor"), PwdState: -1,
	}
	if v := q.Get("status"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || (n != 1 && n != 2) {
			writeError(w, http.StatusBadRequest, "BAD_STATUS", "status must be 1 or 2")
			return
		}
		f.Status = n
	}
	if v := q.Get("pwd_state"); v != "" {
		switch v {
		case "unknown":
			f.PwdState = 0
		case "managed":
			f.PwdState = 1
		case "delivered_removed":
			f.PwdState = 2
		case "all":
			f.PwdState = -1
		default:
			writeError(w, http.StatusBadRequest, "BAD_PWD_STATE", "invalid pwd_state")
			return
		}
	}
	page, err := s.Services.Users.List(r.Context(), f)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type createUserRequest struct {
	UserID          string   `json:"user_id"`
	Name            string   `json:"name"`
	Password        string   `json:"password"`
	MaxConv         int      `json:"max_conv"`
	Tags            []string `json:"tags"`
	Note            string   `json:"note"`
	ClientRequestID string   `json:"client_request_id"`
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.Security.RequireStepUpWrites && !s.checkElevated(w, r) {
		return
	}
	var in createUserRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if in.ClientRequestID == "" {
		in.ClientRequestID = r.Header.Get("Idempotency-Key")
	}
	res, err := s.Services.Users.Create(r.Context(), services.CreateInput{
		UserID: in.UserID, Name: in.Name, Password: in.Password, MaxConv: in.MaxConv,
		Tags: in.Tags, Note: in.Note, RequestID: in.ClientRequestID,
	}, s.meta(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, res)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.Services.Users.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	var in services.PatchInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := s.Services.Users.Patch(r.Context(), r.PathValue("id"), in, s.meta(r)); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type resetPasswordRequest struct {
	Password        string `json:"password"`
	Reason          string `json:"reason"`
	ClientRequestID string `json:"client_request_id"`
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.Security.RequireStepUpWrites && !s.checkElevated(w, r) {
		return
	}
	var in resetPasswordRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if in.ClientRequestID == "" {
		in.ClientRequestID = r.Header.Get("Idempotency-Key")
	}
	res, err := s.Services.Users.ResetPassword(r.Context(), r.PathValue("id"), in.Password, in.Reason, in.ClientRequestID, s.meta(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

type statusRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) disableUser(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.Security.RequireStepUpWrites && !s.checkElevated(w, r) {
		return
	}
	var in statusRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			writeServiceError(w, err)
			return
		}
	}
	if err := s.Services.Users.SetStatus(r.Context(), r.PathValue("id"), 2, in.Reason, requestID(r), s.meta(r)); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enableUser(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.Security.RequireStepUpWrites && !s.checkElevated(w, r) {
		return
	}
	if err := s.Services.Users.SetStatus(r.Context(), r.PathValue("id"), 1, "", requestID(r), s.meta(r)); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type revealRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) revealPassword(w http.ResponseWriter, r *http.Request) {
	if !s.checkElevated(w, r) {
		return
	}
	p := principal(r)
	if p == nil || !s.allowReveal(p.Admin.AdminID) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many password reveals; try again later")
		return
	}
	var in revealRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REASON", "reason is required")
		return
	}
	res, err := s.Services.Users.Reveal(r.Context(), r.PathValue("id"), strings.TrimSpace(in.Reason), s.meta(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.Security.RequireStepUpWrites && !s.checkElevated(w, r) {
		return
	}
	if err := s.Services.Users.RevokeSessions(r.Context(), r.PathValue("id"), s.meta(r)); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) userConversations(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, err := s.Services.Users.Get(r.Context(), userID); err != nil {
		writeServiceError(w, err)
		return
	}
	items, err := s.Services.Store.UserConversations(r.Context(), userID, intQuery(r, "limit", 20, 100))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) userRuns(w http.ResponseWriter, r *http.Request) {
	page, err := s.Services.Runs.List(r.Context(), store.RunFilters{UserID: r.PathValue("id"), Status: r.URL.Query().Get("status"), Limit: intQuery(r, "limit", 20, 100), Cursor: r.URL.Query().Get("cursor")})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) userUsage(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from := now.Add(-30 * 24 * time.Hour)
	items, total, err := s.Services.Store.UsageSummary(r.Context(), store.UsageFilters{From: from, To: now.Add(time.Minute), UserID: r.PathValue("id")}, "model")
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": now, "items": items, "total": total})
}
