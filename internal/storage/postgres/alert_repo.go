package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type AlertRepo struct {
	db *DB
}

func NewAlertRepo(db *DB) *AlertRepo {
	return &AlertRepo{db: db}
}

func (r *AlertRepo) Create(ctx context.Context, a *contracts.NormalizedAlert) error {
	labels, err := json.Marshal(a.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	annotations, err := json.Marshal(a.Annotations)
	if err != nil {
		return fmt.Errorf("marshal annotations: %w", err)
	}
	entityHints, err := json.Marshal(a.EntityHints)
	if err != nil {
		return fmt.Errorf("marshal entity_hints: %w", err)
	}
	links, err := json.Marshal(a.Links)
	if err != nil {
		return fmt.Errorf("marshal links: %w", err)
	}

	_, err = r.db.pool.Exec(ctx, `
		INSERT INTO alerts (
			id, tenant_id, source, status, severity, title, summary,
			fingerprint, group_key, starts_at, ends_at,
			labels, annotations, entity_hints, links, raw_ref
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		a.ID, a.TenantID, a.Source, a.Status, a.Severity, a.Title, a.Summary,
		a.Fingerprint, a.GroupKey, a.StartsAt, a.EndsAt,
		labels, annotations, entityHints, links, a.RawRef,
	)
	if err != nil {
		return fmt.Errorf("insert alert: %w", err)
	}
	return nil
}

func (r *AlertRepo) GetByID(ctx context.Context, id string) (*contracts.NormalizedAlert, error) {
	return r.scanAlert(r.db.pool.QueryRow(ctx, `
		SELECT id, tenant_id, source, status, severity, title, summary,
		       fingerprint, group_key, starts_at, ends_at,
		       labels, annotations, entity_hints, links, raw_ref
		FROM alerts WHERE id = $1`, id))
}

func (r *AlertRepo) GetByFingerprint(ctx context.Context, tenantID, fingerprint string) (*contracts.NormalizedAlert, error) {
	return r.scanAlert(r.db.pool.QueryRow(ctx, `
		SELECT id, tenant_id, source, status, severity, title, summary,
		       fingerprint, group_key, starts_at, ends_at,
		       labels, annotations, entity_hints, links, raw_ref
		FROM alerts WHERE tenant_id = $1 AND fingerprint = $2`, tenantID, fingerprint))
}

func (r *AlertRepo) scanAlert(row pgx.Row) (*contracts.NormalizedAlert, error) {
	var a contracts.NormalizedAlert
	var labelsRaw, annotationsRaw, entityHintsRaw, linksRaw []byte

	err := row.Scan(
		&a.ID, &a.TenantID, &a.Source, &a.Status, &a.Severity, &a.Title, &a.Summary,
		&a.Fingerprint, &a.GroupKey, &a.StartsAt, &a.EndsAt,
		&labelsRaw, &annotationsRaw, &entityHintsRaw, &linksRaw, &a.RawRef,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("alert not found")
		}
		return nil, fmt.Errorf("scan alert: %w", err)
	}
	if labelsRaw != nil {
		if err := json.Unmarshal(labelsRaw, &a.Labels); err != nil {
			return nil, fmt.Errorf("unmarshal labels: %w", err)
		}
	}
	if annotationsRaw != nil {
		if err := json.Unmarshal(annotationsRaw, &a.Annotations); err != nil {
			return nil, fmt.Errorf("unmarshal annotations: %w", err)
		}
	}
	if entityHintsRaw != nil {
		if err := json.Unmarshal(entityHintsRaw, &a.EntityHints); err != nil {
			return nil, fmt.Errorf("unmarshal entity_hints: %w", err)
		}
	}
	if linksRaw != nil {
		if err := json.Unmarshal(linksRaw, &a.Links); err != nil {
			return nil, fmt.Errorf("unmarshal links: %w", err)
		}
	}
	return &a, nil
}
