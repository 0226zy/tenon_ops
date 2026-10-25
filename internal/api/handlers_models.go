package api

import (
	"io"
	"net/http"
	"time"

	"github.com/0226zy/tenon_ops/internal/services"
)

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	items, err := s.Services.Models.List(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type importModelsRequest struct {
	YAML string `json:"yaml"`
}

func (s *Server) importModels(w http.ResponseWriter, r *http.Request) {
	var yamlText string
	if r.Header.Get("Content-Type") == "application/x-yaml" || r.Header.Get("Content-Type") == "text/yaml" {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "TOO_LARGE", "catalog yaml is too large")
			return
		}
		yamlText = string(body)
	} else {
		var in importModelsRequest
		if err := decodeJSON(w, r, &in); err != nil {
			writeServiceError(w, err)
			return
		}
		yamlText = in.YAML
	}
	out, err := s.Services.Models.Import(r.Context(), yamlText, s.meta(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getModel(w http.ResponseWriter, r *http.Request) {
	v, prices, err := s.Services.Models.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": v, "prices": prices})
}

func (s *Server) patchModel(w http.ResponseWriter, r *http.Request) {
	var in services.ModelPatch
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := s.Services.Models.Patch(r.Context(), r.PathValue("id"), in, s.meta(r)); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) modelPrices(w http.ResponseWriter, r *http.Request) {
	_, prices, err := s.Services.Models.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": prices})
}

type addPriceRequest struct {
	EffectiveFrom string  `json:"effective_from"`
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
	Currency      string  `json:"currency"`
}

func (s *Server) addModelPrice(w http.ResponseWriter, r *http.Request) {
	var in addPriceRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, err)
		return
	}
	var effective time.Time
	if in.EffectiveFrom != "" {
		t, err := parseAPITime(in.EffectiveFrom)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_TIME", "invalid effective_from")
			return
		}
		effective = t
	}
	p, err := s.Services.Models.AddPrice(r.Context(), r.PathValue("id"), services.PriceInput{
		EffectiveFrom: effective, InputPerMTok: in.InputPerMTok, OutputPerMTok: in.OutputPerMTok, Currency: in.Currency,
	}, s.meta(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) modelUsage(w http.ResponseWriter, r *http.Request) {
	from, to, err := s.Services.Usage.Range(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	items, total, err := s.Services.Models.Usage(r.Context(), r.PathValue("id"), from, to)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "items": items, "total": total})
}

func parseAPITime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", v)
	return t.UTC(), err
}
