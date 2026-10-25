package api

import (
	"net/http"
	"strconv"

	"github.com/0226zy/tenon_ops/internal/store"
)

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AuditFilters{AdminID: q.Get("admin_id"), Action: q.Get("action"), Target: q.Get("target"), Cursor: q.Get("cursor"), Limit: intQuery(r, "limit", 50, 200), Result: -1}
	if v := q.Get("result"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || (n != 0 && n != 1) {
			writeError(w, http.StatusBadRequest, "BAD_RESULT", "result must be 0 or 1")
			return
		}
		f.Result = n
	}
	if v := q.Get("from"); v != "" {
		t, err := parseAPITime(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_TIME", "invalid from")
			return
		}
		f.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := parseAPITime(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_TIME", "invalid to")
			return
		}
		f.To = &t
	}
	page, err := s.Services.Audit.List(r.Context(), f)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) auditActions(w http.ResponseWriter, r *http.Request) {
	items, err := s.Services.Audit.Actions(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) auditExport(w http.ResponseWriter, r *http.Request) {
	if !s.checkElevated(w, r) {
		return
	}
	q := r.URL.Query()
	f := store.AuditFilters{AdminID: q.Get("admin_id"), Action: q.Get("action"), Target: q.Get("target"), Limit: 200, Result: -1}
	data, rows, err := s.Services.Audit.ExportCSV(r.Context(), f, s.meta(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=tenon-ops-audit.csv")
	w.Header().Set("X-Row-Count", strconv.Itoa(rows))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
