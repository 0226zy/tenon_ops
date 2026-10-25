package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0226zy/tenon_ops/internal/idgen"
	"github.com/0226zy/tenon_ops/internal/security"
	"github.com/0226zy/tenon_ops/internal/store"
	"golang.org/x/crypto/argon2"
)

type Users struct{ s *Services }

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

type UserView struct {
	UserID         string     `json:"user_id"`
	Name           string     `json:"name"`
	Status         int        `json:"status"`
	StatusText     string     `json:"status_text"`
	MaxConv        int        `json:"max_conv"`
	CreatedAt      time.Time  `json:"created_at"`
	LastSeen       *time.Time `json:"last_seen,omitempty"`
	Sessions       int        `json:"sessions"`
	ActiveRuns     int        `json:"active_runs"`
	Conversations  int        `json:"conversations"`
	Tokens30d      int64      `json:"tokens_30d"`
	PwdState       string     `json:"pwd_state"`
	PwdManaged     bool       `json:"pwd_managed"`
	Tags           []string   `json:"tags"`
	Note           string     `json:"note"`
	CreatedBy      string     `json:"created_by,omitempty"`
	LastRevealAt   *time.Time `json:"last_reveal_at,omitempty"`
	RevealCount    int        `json:"reveal_count"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
}

type UserDetail struct {
	UserView
	RecentConversations []store.ConversationBrief `json:"recent_conversations"`
	RecentRuns          []RunView                 `json:"recent_runs"`
	UsageByModel        []store.UsageAgg          `json:"usage_by_model"`
}

type CreateInput struct {
	UserID    string   `json:"user_id"`
	Name      string   `json:"name"`
	Password  string   `json:"password"`
	MaxConv   int      `json:"max_conv"`
	Tags      []string `json:"tags"`
	Note      string   `json:"note"`
	RequestID string   `json:"client_request_id"`
}

type CreateResult struct {
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Password  string    `json:"password,omitempty"`
	MaxConv   int       `json:"max_conv"`
	Replayed  bool      `json:"replayed,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ResetResult struct {
	UserID   string `json:"user_id"`
	Password string `json:"password,omitempty"`
	Replayed bool   `json:"replayed,omitempty"`
}

type RevealResult struct {
	UserID    string `json:"user_id"`
	Password  string `json:"password"`
	ExpiresIn int    `json:"expires_in"`
}

type PatchInput struct {
	Name    *string   `json:"name"`
	MaxConv *int      `json:"max_conv"`
	Note    *string   `json:"note"`
	Tags    *[]string `json:"tags"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func (u *Users) List(ctx context.Context, f store.UserFilters) (Page[UserView], error) {
	if f.PwdState == 0 {
		// 0 is a valid state (unknown); callers must use -1 for "all".
	}
	rows, next, err := u.s.Store.ListUsers(ctx, f)
	if err != nil {
		return Page[UserView]{}, Wrap(err, "list users")
	}
	views := make([]UserView, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		views = append(views, u.view(r))
		ids = append(ids, r.UserID)
	}
	if err := u.enrich(ctx, ids, views); err != nil {
		return Page[UserView]{}, err
	}
	return Page[UserView]{Items: views, NextCursor: next}, nil
}

func (u *Users) Get(ctx context.Context, userID string) (*UserDetail, error) {
	row, err := u.s.Store.UserWithProfile(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, NotFound("user not found")
		}
		return nil, Wrap(err, "get user")
	}
	d := &UserDetail{UserView: u.view(*row)}
	lastSeen, sessions, activeRuns, convs, tokens, err := u.s.Store.UserActivity(ctx, userID)
	if err != nil {
		return nil, Wrap(err, "user activity")
	}
	d.LastSeen, d.Sessions, d.ActiveRuns, d.Conversations, d.Tokens30d = lastSeen, sessions, activeRuns, convs, tokens
	d.RecentConversations, err = u.s.Store.UserConversations(ctx, userID, 20)
	if err != nil {
		return nil, Wrap(err, "user conversations")
	}
	runs, _, err := u.s.Store.ListRuns(ctx, store.RunFilters{UserID: userID, Limit: 15})
	if err != nil {
		return nil, Wrap(err, "user runs")
	}
	d.RecentRuns = make([]RunView, 0, len(runs))
	for _, run := range runs {
		d.RecentRuns = append(d.RecentRuns, runView(run))
	}
	now := time.Now().UTC()
	usage, _, err := u.s.Store.UsageSummary(ctx, store.UsageFilters{From: now.Add(-30 * 24 * time.Hour), To: now.Add(time.Minute), UserID: userID}, "model")
	if err != nil {
		return nil, Wrap(err, "user usage")
	}
	d.UsageByModel = usage
	return d, nil
}

func (u *Users) view(r store.UserWithProfile) UserView {
	v := UserView{
		UserID: r.UserID, Name: r.Name, Status: r.Status, StatusText: statusText(r.Status),
		MaxConv: r.MaxConv, CreatedAt: r.CreatedAt,
		PwdState: "unknown", Tags: []string{},
	}
	if r.Profile != nil {
		v.Tags = r.Profile.Tags
		if v.Tags == nil {
			v.Tags = []string{}
		}
		v.Note = r.Profile.Note
		v.CreatedBy = r.Profile.CreatedBy
		v.LastRevealAt = r.Profile.LastRevealAt
		v.RevealCount = r.Profile.RevealCount
		v.DisabledReason = r.Profile.DisabledReason
		switch r.Profile.PwdState {
		case 1:
			v.PwdState = "managed"
		case 2:
			v.PwdState = "delivered_removed"
		}
	}
	if r.HasSecret {
		v.PwdState = "managed"
	}
	if v.PwdState == "managed" && !r.HasSecret {
		v.PwdState = "unknown"
	}
	v.PwdManaged = v.PwdState == "managed"
	return v
}

func statusText(s int) string {
	if s == 1 {
		return "active"
	}
	if s == 2 {
		return "disabled"
	}
	return "unknown"
}

func (u *Users) enrich(ctx context.Context, ids []string, views []UserView) error {
	if len(ids) == 0 {
		return nil
	}
	in := inClause(len(ids))
	args := anyArgs(ids)
	// active runs
	rows, err := u.s.Store.DB().QueryContext(ctx, `SELECT Fuser_id,COUNT(*) FROM `+tableName(u.s.Cfg.DB.TenonSchema, "t_run")+` WHERE Factive=1 AND Fuser_id IN (`+in+`) GROUP BY Fuser_id`, args...)
	if err != nil {
		return Wrap(err, "active runs")
	}
	active := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return Wrap(err, "active runs")
		}
		active[id] = n
	}
	rows.Close()
	// sessions
	rows, err = u.s.Store.DB().QueryContext(ctx, `SELECT Fuser_id,COUNT(*) FROM `+tableName(u.s.Cfg.DB.TenonSchema, "t_auth_session")+` WHERE Fexpire_time>? AND Fuser_id IN (`+in+`) GROUP BY Fuser_id`, append([]any{time.Now().UTC()}, args...)...)
	if err != nil {
		return Wrap(err, "sessions")
	}
	sessions := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return Wrap(err, "sessions")
		}
		sessions[id] = n
	}
	rows.Close()
	// tokens 30d
	rows, err = u.s.Store.DB().QueryContext(ctx, `SELECT Fuser_id,COALESCE(SUM(Fprompt_tokens+Fcompl_tokens),0) FROM `+tableName(u.s.Cfg.DB.TenonSchema, "t_llm_usage")+` WHERE Fcreate_time>=? AND Fuser_id IN (`+in+`) GROUP BY Fuser_id`, append([]any{time.Now().UTC().Add(-30 * 24 * time.Hour)}, args...)...)
	if err != nil {
		return Wrap(err, "tokens")
	}
	tokens := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return Wrap(err, "tokens")
		}
		tokens[id] = n
	}
	rows.Close()
	// last activity
	rows, err = u.s.Store.DB().QueryContext(ctx, `SELECT Fuser_id,MAX(Flast_active_time) FROM `+tableName(u.s.Cfg.DB.TenonSchema, "t_conversation")+` WHERE Flstate=1 AND Fuser_id IN (`+in+`) GROUP BY Fuser_id`, args...)
	if err != nil {
		return Wrap(err, "last activity")
	}
	last := map[string]time.Time{}
	for rows.Next() {
		var id string
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			rows.Close()
			return Wrap(err, "last activity")
		}
		last[id] = t
	}
	rows.Close()
	for i := range views {
		views[i].ActiveRuns = active[views[i].UserID]
		views[i].Sessions = sessions[views[i].UserID]
		views[i].Tokens30d = tokens[views[i].UserID]
		if t, ok := last[views[i].UserID]; ok {
			tt := t
			views[i].LastSeen = &tt
		}
	}
	return nil
}

func (u *Users) Create(ctx context.Context, in CreateInput, meta Meta) (*CreateResult, error) {
	in.UserID = strings.TrimSpace(in.UserID)
	if !usernamePattern.MatchString(in.UserID) {
		return nil, BadRequest("BAD_USER", "username must be 1-64 characters of letters, digits, dot, dash or underscore")
	}
	if in.Name == "" {
		in.Name = in.UserID
	}
	if utf8.RuneCountInString(in.Name) > 64 {
		return nil, BadRequest("BAD_NAME", "display name must be at most 64 characters")
	}
	if in.MaxConv <= 0 {
		in.MaxConv = 20
	}
	if in.MaxConv > 1000 {
		return nil, BadRequest("BAD_MAX_CONV", "max_conv must be between 1 and 1000")
	}
	in.Tags = cleanTags(in.Tags)
	if len(in.Note) > 255 {
		return nil, BadRequest("BAD_NOTE", "note must be at most 255 characters")
	}
	generated := false
	if in.Password == "" {
		p, err := security.RandomPassword()
		if err != nil {
			return nil, Wrap(err, "generate password")
		}
		in.Password = p
		generated = true
	}
	_ = generated
	if err := validateUserPassword(in.Password); err != nil {
		return nil, err
	}
	if _, err := u.s.Store.UserByID(ctx, in.UserID); err == nil {
		return nil, Conflict("USER_EXISTS", "user already exists")
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, Wrap(err, "check user")
	}
	requestID := in.RequestID
	if requestID == "" {
		requestID = idgen.New()
	}
	if op, err := u.s.Store.Operation(ctx, requestID); err == nil {
		if op.Kind != "user.create" {
			return nil, Conflict("CONFLICT", "request id already used")
		}
		if op.Status == 1 {
			return &CreateResult{UserID: op.TargetID, Name: in.Name, MaxConv: in.MaxConv, Replayed: true, CreatedAt: time.Now().UTC()}, nil
		}
		return nil, Conflict("CONFLICT", "request is still in progress")
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, Wrap(err, "check idempotency")
	}
	hash, err := tenonPasswordHash(in.Password)
	if err != nil {
		return nil, Wrap(err, "hash password")
	}
	nonce, ciphertext, err := u.s.Vault.Seal(in.UserID, security.VaultKeyVersion, in.Password)
	if err != nil {
		return nil, E(500, "ENCRYPTION_FAILED", "failed to encrypt password")
	}
	op := &store.Operation{RequestID: requestID, Kind: "user.create", TargetID: in.UserID, RequestHash: hash, Status: 0, ExpireAt: time.Now().UTC().Add(24 * time.Hour)}
	var result CreateResult
	err = u.s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := u.s.Store.BeginOperation(ctx, op, tx); err != nil {
			return err
		}
		user := &store.User{UserID: in.UserID, Name: in.Name, PwdHash: hash, Status: 1, MaxConv: in.MaxConv}
		if err := u.s.Store.CreateUserTx(ctx, tx, user); err != nil {
			return err
		}
		profile := &store.UserProfile{UserID: in.UserID, Note: in.Note, Tags: in.Tags, PwdState: 1, CreatedBy: meta.AdminID}
		if err := u.s.Store.UpsertProfileTx(ctx, tx, profile); err != nil {
			return err
		}
		if err := u.s.Store.PutSecretTx(ctx, tx, &store.Secret{UserID: in.UserID, KeyVersion: security.VaultKeyVersion, Nonce: nonce, Ciphertext: ciphertext, CreatedBy: meta.AdminID}); err != nil {
			return err
		}
		if err := u.s.Store.InsertAuditTx(ctx, tx, &store.AuditEntry{TS: time.Now().UTC(), AdminID: meta.AdminID, Action: "user.create", TargetType: "user", TargetID: in.UserID, Result: 1, IP: meta.IP, UA: meta.UA, RequestID: requestID, Detail: mustJSON(map[string]any{"name": in.Name, "max_conv": in.MaxConv, "tags": in.Tags})}); err != nil {
			return err
		}
		body := mustJSON(map[string]any{"user_id": in.UserID})
		if err := u.s.Store.CompleteOperation(ctx, tx, requestID, 1, body); err != nil {
			return err
		}
		result = CreateResult{UserID: in.UserID, Name: in.Name, Password: in.Password, MaxConv: in.MaxConv, CreatedAt: user.CreatedAt}
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return nil, Conflict("USER_EXISTS", "user already exists or request already processed")
		}
		return nil, Wrap(err, "create user")
	}
	return &result, nil
}

func (u *Users) ResetPassword(ctx context.Context, userID, password, reason, requestID string, meta Meta) (*ResetResult, error) {
	if requestID == "" {
		requestID = idgen.New()
	}
	if op, err := u.s.Store.Operation(ctx, requestID); err == nil {
		if op.Kind != "user.reset_password" {
			return nil, Conflict("CONFLICT", "request id already used")
		}
		if op.Status == 1 {
			return &ResetResult{UserID: op.TargetID, Replayed: true}, nil
		}
		return nil, Conflict("CONFLICT", "request is still in progress")
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, Wrap(err, "check idempotency")
	}
	if password == "" {
		p, err := security.RandomPassword()
		if err != nil {
			return nil, Wrap(err, "generate password")
		}
		password = p
	}
	if err := validateUserPassword(password); err != nil {
		return nil, err
	}
	hash, err := tenonPasswordHash(password)
	if err != nil {
		return nil, Wrap(err, "hash password")
	}
	nonce, ciphertext, err := u.s.Vault.Seal(userID, security.VaultKeyVersion, password)
	if err != nil {
		return nil, E(500, "ENCRYPTION_FAILED", "failed to encrypt password")
	}
	op := &store.Operation{RequestID: requestID, Kind: "user.reset_password", TargetID: userID, RequestHash: hash, Status: 0, ExpireAt: time.Now().UTC().Add(24 * time.Hour)}
	err = u.s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := u.s.Store.BeginOperation(ctx, op, tx); err != nil {
			return err
		}
		user, err := u.s.Store.LockUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := u.s.Store.UpdateUserPasswordTx(ctx, tx, userID, hash); err != nil {
			return err
		}
		if err := u.s.Store.ExpireUserSessionsTx(ctx, tx, userID); err != nil {
			return err
		}
		profile, err := u.s.Store.Profile(ctx, userID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if profile == nil {
			profile = &store.UserProfile{UserID: userID, CreatedBy: meta.AdminID}
		}
		profile.PwdState = 1
		if err := u.s.Store.UpsertProfileTx(ctx, tx, profile); err != nil {
			return err
		}
		if err := u.s.Store.PutSecretTx(ctx, tx, &store.Secret{UserID: userID, KeyVersion: security.VaultKeyVersion, Nonce: nonce, Ciphertext: ciphertext, CreatedBy: meta.AdminID}); err != nil {
			return err
		}
		if err := u.s.Store.InsertAuditTx(ctx, tx, &store.AuditEntry{TS: time.Now().UTC(), AdminID: meta.AdminID, Action: "user.reset_password", TargetType: "user", TargetID: userID, Result: 1, IP: meta.IP, UA: meta.UA, RequestID: requestID, Reason: reason, Detail: mustJSON(map[string]any{"previous_status": user.Status})}); err != nil {
			return err
		}
		return u.s.Store.CompleteOperation(ctx, tx, requestID, 1, mustJSON(map[string]any{"user_id": userID}))
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, NotFound("user not found")
		}
		if errors.Is(err, store.ErrDuplicate) {
			return nil, Conflict("CONFLICT", "request already processed")
		}
		return nil, Wrap(err, "reset password")
	}
	return &ResetResult{UserID: userID, Password: password}, nil
}

func (u *Users) SetStatus(ctx context.Context, userID string, status int, reason, requestID string, meta Meta) error {
	err := u.s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		user, err := u.s.Store.LockUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := u.s.Store.UpdateUserStatusTx(ctx, tx, userID, status); err != nil {
			return err
		}
		if status == 2 {
			if err := u.s.Store.ExpireUserSessionsTx(ctx, tx, userID); err != nil {
				return err
			}
		}
		profile, err := u.s.Store.Profile(ctx, userID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if profile == nil {
			profile = &store.UserProfile{UserID: userID, CreatedBy: meta.AdminID}
		}
		if status == 2 {
			profile.DisabledReason = reason
		} else {
			profile.DisabledReason = ""
		}
		if err := u.s.Store.UpsertProfileTx(ctx, tx, profile); err != nil {
			return err
		}
		action := "user.enable"
		if status == 2 {
			action = "user.disable"
		}
		return u.s.Store.InsertAuditTx(ctx, tx, &store.AuditEntry{TS: time.Now().UTC(), AdminID: meta.AdminID, Action: action, TargetType: "user", TargetID: userID, Result: 1, IP: meta.IP, UA: meta.UA, RequestID: requestID, Reason: reason, Detail: mustJSON(map[string]any{"previous_status": user.Status})})
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("user not found")
		}
		return Wrap(err, "update user status")
	}
	return nil
}

func (u *Users) Reveal(ctx context.Context, userID, reason string, meta Meta) (*RevealResult, error) {
	if _, err := u.s.Store.UserByID(ctx, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, NotFound("user not found")
		}
		return nil, Wrap(err, "get user")
	}
	secret, err := u.s.Store.Secret(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrPasswordNotManaged
		}
		return nil, Wrap(err, "get secret")
	}
	password, err := u.s.Vault.Open(userID, secret.KeyVersion, secret.Nonce, secret.Ciphertext)
	if err != nil {
		return nil, E(500, "VAULT_DECRYPT_FAILED", "password cannot be decrypted; reset it")
	}
	if err := u.s.Store.InsertAudit(ctx, &store.AuditEntry{TS: time.Now().UTC(), AdminID: meta.AdminID, Action: "user.reveal_password", TargetType: "user", TargetID: userID, Result: 1, IP: meta.IP, UA: meta.UA, RequestID: meta.RequestID, Reason: reason}); err != nil {
		return nil, Wrap(err, "audit reveal")
	}
	_ = u.s.Store.MarkReveal(ctx, userID)
	return &RevealResult{UserID: userID, Password: password, ExpiresIn: 30}, nil
}

func (u *Users) Patch(ctx context.Context, userID string, in PatchInput, meta Meta) error {
	if in.Name != nil && utf8.RuneCountInString(*in.Name) > 64 {
		return BadRequest("BAD_NAME", "display name must be at most 64 characters")
	}
	if in.MaxConv != nil && (*in.MaxConv < 1 || *in.MaxConv > 1000) {
		return BadRequest("BAD_MAX_CONV", "max_conv must be between 1 and 1000")
	}
	if in.Note != nil && len(*in.Note) > 255 {
		return BadRequest("BAD_NOTE", "note must be at most 255 characters")
	}
	if in.Tags != nil {
		cleaned := cleanTags(*in.Tags)
		in.Tags = &cleaned
	}
	err := u.s.Store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := u.s.Store.LockUser(ctx, tx, userID); err != nil {
			return err
		}
		if in.Name != nil {
			if err := u.s.Store.UpdateUserNameTx(ctx, tx, userID, *in.Name); err != nil {
				return err
			}
		}
		if in.MaxConv != nil {
			if err := u.s.Store.UpdateUserMaxConvTx(ctx, tx, userID, *in.MaxConv); err != nil {
				return err
			}
		}
		if in.Note != nil || in.Tags != nil {
			profile, err := u.s.Store.Profile(ctx, userID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if profile == nil {
				profile = &store.UserProfile{UserID: userID, CreatedBy: meta.AdminID}
			}
			if in.Note != nil {
				profile.Note = *in.Note
			}
			if in.Tags != nil {
				profile.Tags = *in.Tags
			}
			if err := u.s.Store.UpsertProfileTx(ctx, tx, profile); err != nil {
				return err
			}
		}
		return u.s.Store.InsertAuditTx(ctx, tx, &store.AuditEntry{TS: time.Now().UTC(), AdminID: meta.AdminID, Action: "user.meta.update", TargetType: "user", TargetID: userID, Result: 1, IP: meta.IP, UA: meta.UA, RequestID: meta.RequestID, Detail: mustJSON(map[string]any{"name": in.Name, "max_conv": in.MaxConv, "note": in.Note, "tags": in.Tags})})
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("user not found")
		}
		return Wrap(err, "patch user")
	}
	return nil
}

func (u *Users) RevokeSessions(ctx context.Context, userID string, meta Meta) error {
	if _, err := u.s.Store.UserByID(ctx, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("user not found")
		}
		return Wrap(err, "get user")
	}
	if err := u.s.Store.ExpireUserSessions(ctx, userID); err != nil {
		return Wrap(err, "revoke sessions")
	}
	return u.s.audit(ctx, meta, "user.sessions.revoke", "user", userID, 1, "", nil)
}

func tenonPasswordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", 64*1024, 2, 2,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func validateUserPassword(p string) error {
	if len(p) < 8 {
		return BadRequest("WEAK_PASSWORD", "password must contain at least 8 bytes")
	}
	if len(p) > 128 {
		return BadRequest("LONG_PASSWORD", "password must contain at most 128 bytes")
	}
	return nil
}

func cleanTags(in []string) []string {
	if in == nil {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || len(v) > 32 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) >= 10 {
			break
		}
	}
	return out
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func inClause(n int) string {
	if n <= 0 {
		return ""
	}
	s := "?"
	for i := 1; i < n; i++ {
		s += ",?"
	}
	return s
}

func anyArgs(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func tableName(schema, table string) string {
	return "`" + schema + "`.`" + table + "`"
}
