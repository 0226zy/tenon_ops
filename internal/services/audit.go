package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/0226zy/tenon_ops/internal/store"
)

type Audit struct{ s *Services }

func (a *Audit) List(ctx context.Context, f store.AuditFilters) (Page[store.AuditEntry], error) {
	items, next, err := a.s.Store.ListAudit(ctx, f)
	if err != nil {
		return Page[store.AuditEntry]{}, Wrap(err, "list audit")
	}
	return Page[store.AuditEntry]{Items: items, NextCursor: next}, nil
}

func (a *Audit) Actions(ctx context.Context) ([]string, error) {
	return a.s.Store.AuditActions(ctx)
}

func (a *Audit) ExportCSV(ctx context.Context, f store.AuditFilters, meta Meta) ([]byte, int, error) {
	f.Limit = 200
	items, _, err := a.s.Store.ListAudit(ctx, f)
	if err != nil {
		return nil, 0, Wrap(err, "audit export")
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ts", "admin_id", "action", "target_type", "target_id", "result", "ip", "request_id", "reason"})
	for _, e := range items {
		_ = w.Write([]string{
			e.TS.UTC().Format(time.RFC3339), csvSafe(e.AdminID), csvSafe(e.Action), e.TargetType, e.TargetID,
			strconv.Itoa(e.Result), e.IP, e.RequestID, csvSafe(e.Reason),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, 0, err
	}
	if err := a.s.audit(ctx, meta, "audit.export", "audit", "", 1, "", map[string]any{"rows": len(items)}); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), len(items), nil
}

var _ = fmt.Sprintf
