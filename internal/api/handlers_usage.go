package api

import (
	"net/http"
	"strconv"

	"github.com/0226zy/tenon_ops/internal/store"
)

func (s *Server) usageFilters(r *http.Request) (store.UsageFilters, error) {
	from, to, err := s.Services.Usage.Range(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		return store.UsageFilters{}, err
	}
	return store.UsageFilters{
		From: from, To: to, UserID: r.URL.Query().Get("user_id"),
		Model: r.URL.Query().Get("model"), ConvID: r.URL.Query().Get("conv_id"),
		Limit: intQuery(r, "limit", 100, 500),
	}, nil
}

func (s *Server) usageOverview(w http.ResponseWriter, r *http.Request) {
	f, err := s.usageFilters(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	out, err := s.Services.Usage.Overview(r.Context(), f, r.URL.Query().Get("group_by"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) usageExport(w http.ResponseWriter, r *http.Request) {
	if !s.checkElevated(w, r) {
		return
	}
	f, err := s.usageFilters(r)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	data, rows, err := s.Services.Usage.ExportCSV(r.Context(), f, s.meta(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=tenon-ops-usage.csv")
	w.Header().Set("X-Row-Count", strconv.Itoa(rows))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
