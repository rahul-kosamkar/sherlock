package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type AuditRepo struct {
	db *DB
}

func NewAuditRepo(db *DB) *AuditRepo {
	return &AuditRepo{db: db}
}

func (r *AuditRepo) Create(ctx context.Context, entry *contracts.AuditEntry) error {
	meta, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = r.db.pool.Exec(ctx, `
		INSERT INTO audit_log (id, tenant_id, actor, action, target, metadata, timestamp)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		entry.ID, entry.TenantID, entry.Actor, entry.Action,
		entry.Target, meta, entry.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

func (r *AuditRepo) List(ctx context.Context, tenantID string, limit int) ([]contracts.AuditEntry, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT id, tenant_id, actor, action, target, metadata, timestamp
		FROM audit_log
		WHERE tenant_id = $1
		ORDER BY timestamp DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()

	var result []contracts.AuditEntry
	for rows.Next() {
		var e contracts.AuditEntry
		var metaRaw []byte
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.Actor, &e.Action, &e.Target, &metaRaw, &e.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		if metaRaw != nil {
			if err := json.Unmarshal(metaRaw, &e.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
