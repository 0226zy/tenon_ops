package services

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
	"gopkg.in/yaml.v3"
)

type Models struct{ s *Services }

type ModelView struct {
	store.Model
	Price       *store.Price `json:"price,omitempty"`
	Requests24h int64        `json:"requests_24h"`
	Tokens24h   int64        `json:"tokens_24h"`
	Errors24h   int64        `json:"errors_24h"`
	Cost24h     float64      `json:"cost_24h"`
	Ready       string       `json:"ready"` // unknown | ready | missing_env
	ReadyBasis  string       `json:"ready_basis"`
	LastUsed    *time.Time   `json:"last_used,omitempty"`
}

type PriceInput struct {
	EffectiveFrom   time.Time `json:"effective_from"`
	InputPerMTok    float64   `json:"input_per_mtok"`     // cache miss, off-peak
	CacheHitPerMTok float64   `json:"cache_hit_per_mtok"` // cache hit, off-peak
	OutputPerMTok   float64   `json:"output_per_mtok"`
	PeakMultiplier  float64   `json:"peak_multiplier"`
	Currency        string    `json:"currency"`
}

type ModelPatch struct {
	DisplayName *string `json:"display_name"`
	Provider    *string `json:"provider"`
	Notes       *string `json:"notes"`
	Enabled     *bool   `json:"enabled"`
}

type catalogDoc struct {
	Models []catalogModel `yaml:"models"`
}

type catalogModel struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	Desc          string   `yaml:"desc"`
	Tags          []string `yaml:"tags"`
	Default       bool     `yaml:"default"`
	Protocol      string   `yaml:"protocol"`
	BaseURL       string   `yaml:"base_url"`
	APIKeyEnv     string   `yaml:"api_key_env"`
	UpstreamModel string   `yaml:"upstream_model"`
	MaxTokens     int      `yaml:"max_tokens"`
	Demo          bool     `yaml:"demo"`
}

func (m *Models) List(ctx context.Context) ([]ModelView, error) {
	models, err := m.s.Store.ListModels(ctx)
	if err != nil {
		return nil, Wrap(err, "list models")
	}
	prices, err := m.s.Store.LatestPriceMap(ctx)
	if err != nil {
		return nil, Wrap(err, "prices")
	}
	now := time.Now().UTC()
	usage, _, err := m.s.Store.UsageSummary(ctx, store.UsageFilters{From: now.Add(-24 * time.Hour), To: now.Add(time.Minute), Offset: offsetString(m.s.Cfg.Location(), now)}, "model")
	if err != nil {
		return nil, Wrap(err, "model usage")
	}
	byModel := map[string]store.UsageAgg{}
	for _, a := range usage {
		byModel[a.Model] = a
	}
	out := make([]ModelView, 0, len(models))
	for _, v := range models {
		view := ModelView{Model: v, Ready: "unknown", ReadyBasis: "control plane not connected"}
		if p, ok := prices[v.ModelID]; ok {
			pp := p
			view.Price = &pp
		}
		if a, ok := byModel[v.ModelID]; ok {
			view.Requests24h, view.Tokens24h, view.Errors24h, view.Cost24h = a.Requests, a.TotalTokens, a.Errors, a.Cost
		}
		out = append(out, view)
	}
	return out, nil
}

func (m *Models) Get(ctx context.Context, id string) (*ModelView, []store.Price, error) {
	v, err := m.s.Store.ModelByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, NotFound("model not found")
		}
		return nil, nil, Wrap(err, "get model")
	}
	prices, err := m.s.Store.Prices(ctx, id)
	if err != nil {
		return nil, nil, Wrap(err, "prices")
	}
	var price *store.Price
	if len(prices) > 0 {
		p := prices[0]
		price = &p
	}
	return &ModelView{Model: *v, Price: price, Ready: "unknown", ReadyBasis: "control plane not connected"}, prices, nil
}

func (m *Models) Import(ctx context.Context, yamlText string, meta Meta) (map[string]any, error) {
	if strings.TrimSpace(yamlText) == "" {
		return nil, BadRequest("BAD_CATALOG", "catalog yaml is empty")
	}
	if len(yamlText) > 1<<20 {
		return nil, BadRequest("CATALOG_TOO_LARGE", "catalog yaml must be at most 1MB")
	}
	var doc catalogDoc
	dec := yaml.NewDecoder(bytes.NewReader([]byte(yamlText)))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, BadRequest("BAD_CATALOG", "invalid model catalog: "+err.Error())
	}
	if len(doc.Models) == 0 {
		return nil, BadRequest("BAD_CATALOG", "model catalog is empty")
	}
	now := time.Now().UTC()
	seen := map[string]bool{}
	defaults := 0
	models := make([]store.Model, 0, len(doc.Models))
	for _, c := range doc.Models {
		id := strings.TrimSpace(c.ID)
		if id == "" || seen[id] {
			return nil, BadRequest("BAD_CATALOG", "model id is empty or duplicated: "+id)
		}
		seen[id] = true
		if c.Protocol == "" {
			c.Protocol = "openai"
		}
		if c.Demo || c.Protocol == "demo" {
			c.Demo = true
		} else if c.BaseURL == "" || c.Protocol != "openai" {
			return nil, BadRequest("BAD_CATALOG", "model "+id+" must use an OpenAI-compatible base_url")
		}
		if c.Default {
			defaults++
		}
		name := c.Name
		if name == "" {
			name = id
		}
		upstream := c.UpstreamModel
		if upstream == "" {
			upstream = id
		}
		m := store.Model{
			ModelID: id, DisplayName: name, Provider: providerOf(c.BaseURL), UpstreamModel: upstream,
			BaseURL: c.BaseURL, APIKeyEnv: c.APIKeyEnv, Tags: c.Tags, MaxOutput: c.MaxTokens,
			Enabled: true, IsDefault: c.Default, Source: "yaml", Notes: c.Desc, SyncedAt: &now,
		}
		if m.Tags == nil {
			m.Tags = []string{}
		}
		models = append(models, m)
	}
	if defaults > 1 {
		return nil, BadRequest("BAD_CATALOG", "multiple default models")
	}
	if defaults == 0 {
		return nil, BadRequest("BAD_CATALOG", "catalog needs exactly one default model")
	}
	keep := make([]string, 0, len(models))
	for _, v := range models {
		keep = append(keep, v.ModelID)
	}
	err := m.s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		for i := range models {
			if err := m.s.Store.UpsertModelTx(ctx, tx, &models[i]); err != nil {
				return err
			}
		}
		if err := m.s.Store.DisableModelsTx(ctx, tx, keep); err != nil {
			return err
		}
		return m.s.Store.InsertAuditTx(ctx, tx, &store.AuditEntry{TS: time.Now().UTC(), AdminID: meta.AdminID, Action: "model.import", TargetType: "model", TargetID: "", Result: 1, IP: meta.IP, UA: meta.UA, RequestID: meta.RequestID, Detail: mustJSON(map[string]any{"models": keep})})
	})
	if err != nil {
		return nil, Wrap(err, "import models")
	}
	return map[string]any{"imported": len(models), "models": keep}, nil
}

func providerOf(baseURL string) string {
	if baseURL == "" {
		return "local"
	}
	return "custom"
}

func (m *Models) Patch(ctx context.Context, id string, in ModelPatch, meta Meta) error {
	cur, err := m.s.Store.ModelByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("model not found")
		}
		return Wrap(err, "get model")
	}
	name, provider, notes, enabled := cur.DisplayName, cur.Provider, cur.Notes, cur.Enabled
	if in.DisplayName != nil {
		name = strings.TrimSpace(*in.DisplayName)
	}
	if in.Provider != nil {
		provider = strings.TrimSpace(*in.Provider)
	}
	if in.Notes != nil {
		notes = strings.TrimSpace(*in.Notes)
	}
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if len(name) > 128 || len(provider) > 64 || len(notes) > 512 {
		return BadRequest("BAD_MODEL", "model fields are too long")
	}
	if err := m.s.Store.UpdateModelMeta(ctx, id, name, provider, notes, enabled); err != nil {
		return Wrap(err, "update model")
	}
	return m.s.audit(ctx, meta, "model.meta.update", "model", id, 1, "", map[string]any{"display_name": name, "provider": provider, "enabled": enabled})
}

func (m *Models) AddPrice(ctx context.Context, id string, in PriceInput, meta Meta) (*store.Price, error) {
	if _, err := m.s.Store.ModelByID(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, NotFound("model not found")
		}
		return nil, Wrap(err, "get model")
	}
	if in.InputPerMTok < 0 || in.CacheHitPerMTok < 0 || in.OutputPerMTok < 0 {
		return nil, BadRequest("BAD_PRICE", "price must be non-negative")
	}
	if in.PeakMultiplier == 0 {
		in.PeakMultiplier = 2.0
	}
	if in.PeakMultiplier < 0.1 || in.PeakMultiplier > 10 {
		return nil, BadRequest("BAD_PRICE", "peak multiplier must be between 0.1 and 10")
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	if len(in.Currency) != 3 {
		return nil, BadRequest("BAD_PRICE", "currency must be a 3-letter code")
	}
	if in.EffectiveFrom.IsZero() {
		in.EffectiveFrom = time.Now().UTC()
	}
	p := &store.Price{ModelID: id, EffectiveFrom: in.EffectiveFrom.UTC(), InputPerMTok: in.InputPerMTok, CacheHitPerMTok: in.CacheHitPerMTok, OutputPerMTok: in.OutputPerMTok, PeakMultiplier: in.PeakMultiplier, Currency: strings.ToUpper(in.Currency), CreatedBy: meta.AdminID}
	if err := m.s.Store.AddPrice(ctx, p); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return nil, Conflict("PRICE_EXISTS", "a price already exists for this effective time")
		}
		return nil, Wrap(err, "add price")
	}
	if err := m.s.audit(ctx, meta, "model.price.create", "model", id, 1, "", map[string]any{"effective_from": p.EffectiveFrom, "cache_miss_input": p.InputPerMTok, "cache_hit_input": p.CacheHitPerMTok, "output": p.OutputPerMTok, "peak_multiplier": p.PeakMultiplier, "currency": p.Currency}); err != nil {
		return nil, err
	}
	return p, nil
}

func (m *Models) Usage(ctx context.Context, id string, from, to time.Time) ([]store.UsageAgg, store.UsageAgg, error) {
	items, total, err := m.s.Store.UsageSummary(ctx, store.UsageFilters{From: from, To: to, Model: id}, "model")
	if err != nil {
		return nil, store.UsageAgg{}, Wrap(err, "model usage")
	}
	return items, total, nil
}

var _ = fmt.Sprintf
