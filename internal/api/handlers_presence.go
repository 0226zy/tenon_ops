package api

import "net/http"

func (s *Server) presence(w http.ResponseWriter, r *http.Request) {
	out, err := s.Services.Presence.List(r.Context(), r.URL.Query().Get("window"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
