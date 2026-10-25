package api

import (
	"net/http"
	"strconv"

	"github.com/0226zy/tenon_ops/internal/store"
)

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.RunFilters{Status: q.Get("status"), UserID: q.Get("user_id"), Model: q.Get("model"), Cursor: q.Get("cursor"), Limit: intQuery(r, "limit", 50, 100)}
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
	page, err := s.Services.Runs.List(r.Context(), f)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	v, err := s.Services.Runs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
	if after < 0 {
		after = 0
	}
	out, err := s.Services.Runs.Events(r.Context(), r.PathValue("id"), after, intQuery(r, "limit", 100, 200))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) runMessages(w http.ResponseWriter, r *http.Request) {
	out, err := s.Services.Runs.Messages(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) runUsage(w http.ResponseWriter, r *http.Request) {
	out, err := s.Services.Runs.Usage(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
