package store

import (
	"encoding/json"
	"time"
)

type Admin struct {
	ID           int64
	AdminID      string
	DisplayName  string
	Role         string
	PwdAlgo      string
	PwdParams    string
	PwdSalt      []byte
	PwdHash      []byte
	PwdVersion   int
	SessionEpoch int64
	Status       int
	FailedCount  int
	LockedUntil  *time.Time
	LastLoginAt  *time.Time
	LastLoginIP  string
	PwdChangedAt time.Time
	CreatedAt    time.Time
	ModifiedAt   time.Time
}

type AdminSession struct {
	ID            int64
	SIDHash       string
	AdminID       string
	ExpireAt      time.Time
	IdleExpireAt  time.Time
	ElevatedUntil *time.Time
	IP            string
	UA            string
	Revoked       int
	CreatedAt     time.Time
	LastSeenAt    *time.Time
}

type User struct {
	ID         int64
	UserID     string
	Name       string
	PwdHash    string
	Status     int
	MaxConv    int
	CreatedAt  time.Time
	ModifiedAt time.Time
}

type UserProfile struct {
	UserID         string
	Note           string
	Tags           []string
	PwdState       int // 0 unknown, 1 managed, 2 delivered_removed
	CreatedBy      string
	LastRevealAt   *time.Time
	RevealCount    int
	DisabledReason string
	CreatedAt      time.Time
	ModifiedAt     time.Time
}

type Secret struct {
	UserID      string
	KeyVersion  int
	Nonce       []byte
	Ciphertext  []byte
	CreatedBy   string
	CreatedAt   time.Time
	RotatedAt   *time.Time
	DeliveredAt *time.Time
}

type Model struct {
	ModelID       string     `json:"model_id"`
	DisplayName   string     `json:"display_name"`
	Provider      string     `json:"provider"`
	UpstreamModel string     `json:"upstream_model"`
	BaseURL       string     `json:"base_url"`
	APIKeyEnv     string     `json:"api_key_env"`
	Tags          []string   `json:"tags"`
	ContextWindow int        `json:"context_window"`
	MaxOutput     int        `json:"max_output"`
	Enabled       bool       `json:"enabled"`
	IsDefault     bool       `json:"is_default"`
	Source        string     `json:"source"`
	Notes         string     `json:"notes"`
	SyncedAt      *time.Time `json:"synced_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ModifiedAt    time.Time  `json:"modified_at"`
}

type Price struct {
	ID              int64     `json:"id"`
	ModelID         string    `json:"model_id"`
	EffectiveFrom   time.Time `json:"effective_from"`
	InputPerMTok    float64   `json:"input_per_mtok"`
	CacheHitPerMTok float64   `json:"cache_hit_per_mtok"`
	OutputPerMTok   float64   `json:"output_per_mtok"`
	PeakMultiplier  float64   `json:"peak_multiplier"`
	Currency        string    `json:"currency"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

type UsageAgg struct {
	UserID           string  `json:"user_id,omitempty"`
	Model            string  `json:"model,omitempty"`
	ConvID           string  `json:"conv_id,omitempty"`
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	ComplTokens      int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Errors           int64   `json:"errors"`
	ErrorRate        float64 `json:"error_rate"`
	Cost             float64 `json:"cost"`
	PricedRequests   int64   `json:"priced_requests"`
	UnpricedRequests int64   `json:"unpriced_requests"`
	AvgMS            float64 `json:"avg_ms"`
}

type UsagePoint struct {
	Day      string  `json:"day"`
	Model    string  `json:"model,omitempty"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Errors   int64   `json:"errors"`
	Cost     float64 `json:"cost"`
}

type Run struct {
	ID            int64
	RunID         string
	ConvID        string
	UserID        string
	UserName      string
	ConvTitle     string
	Model         string
	Status        int
	StatusText    string
	Active        bool
	Generation    int64
	CancelReq     bool
	StopConfirmed bool
	Commit        string
	Iterations    int
	PromptTokens  int
	ComplTokens   int
	ErrorCode     string
	ErrorMessage  string
	StartedAt     *time.Time
	EndedAt       *time.Time
	CreatedAt     time.Time
	ModifiedAt    time.Time
}

type Event struct {
	Seq       int64           `json:"seq"`
	RunID     string          `json:"run_id,omitempty"`
	Source    int             `json:"source"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	Truncated bool            `json:"truncated,omitempty"`
}

type Message struct {
	MsgID     string    `json:"msg_id"`
	Role      int       `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditEntry struct {
	ID         int64           `json:"id"`
	TS         time.Time       `json:"ts"`
	AdminID    string          `json:"admin_id"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Result     int             `json:"result"`
	IP         string          `json:"ip"`
	UA         string          `json:"ua"`
	RequestID  string          `json:"request_id"`
	Reason     string          `json:"reason"`
	Detail     json.RawMessage `json:"detail,omitempty"`
}

type PresenceRow struct {
	UserID       string
	Name         string
	Status       int
	Tier         string
	LastSeen     *time.Time
	ActiveRuns   int
	Sessions     int
	TokensWindow int64
	Tokens24h    int64
	Model        string
}

type ConversationBrief struct {
	ConvID       string    `json:"conv_id"`
	Title        string    `json:"title"`
	Model        string    `json:"model"`
	State        int       `json:"state"`
	LastActiveAt time.Time `json:"last_active_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type RunFilters struct {
	Status string
	UserID string
	Model  string
	From   *time.Time
	To     *time.Time
	Cursor string
	Limit  int
}

type UserFilters struct {
	Query    string
	Status   int
	PwdState int
	Limit    int
	Cursor   string
}
