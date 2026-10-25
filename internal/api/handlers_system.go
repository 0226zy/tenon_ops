package api

import "net/http"

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	out, err := s.Services.Dashboard.Summary(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) systemHealth(w http.ResponseWriter, r *http.Request) {
	out, err := s.Services.System.Health(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	items, err := s.Services.System.Settings(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := s.Services.System.UpdateSettings(r.Context(), in, s.meta(r)); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
