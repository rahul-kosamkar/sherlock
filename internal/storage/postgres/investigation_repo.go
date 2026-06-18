package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type InvestigationRepo struct {
	q Querier
}

func NewInvestigationRepo(db *DB) *InvestigationRepo {
	return &InvestigationRepo{q: db.pool}
}

func (r *InvestigationRepo) WithTx(tx pgx.Tx) *InvestigationRepo {
	return &InvestigationRepo{q: tx}
}

func NewInvestigationRepoTx(tx pgx.Tx) *InvestigationRepo {
	return &InvestigationRepo{q: tx}
}

func (r *InvestigationRepo) Create(ctx context.Context, inv *contracts.Investigation) error {
	targets, err := json.Marshal(inv.Targets)
	if err != nil {
		return fmt.Errorf("marshal targets: %w", err)
	}

	_, err = r.q.Exec(ctx, `
		INSERT INTO investigations (
			id, tenant_id, status, alert_ids, targets,
			time_from, time_to, headline, confidence,
			slack_channel_id, slack_thread_ts,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		inv.ID, inv.TenantID, inv.Status, inv.AlertIDs, targets,
		inv.TimeFrom, inv.TimeTo, inv.Headline, inv.Confidence,
		inv.SlackChannelID, inv.SlackThreadTS,
		inv.CreatedAt, inv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert investigation: %w", err)
	}
	return nil
}

func (r *InvestigationRepo) GetByID(ctx context.Context, id string) (*contracts.Investigation, error) {
	var inv contracts.Investigation
	var targetsRaw []byte

	err := r.q.QueryRow(ctx, `
		SELECT id, tenant_id, status, alert_ids, targets,
		       time_from, time_to, headline, confidence,
		       slack_channel_id, slack_thread_ts,
		       created_at, updated_at, completed_at
		FROM investigations WHERE id = $1`, id,
	).Scan(
		&inv.ID, &inv.TenantID, &inv.Status, &inv.AlertIDs, &targetsRaw,
		&inv.TimeFrom, &inv.TimeTo, &inv.Headline, &inv.Confidence,
		&inv.SlackChannelID, &inv.SlackThreadTS,
		&inv.CreatedAt, &inv.UpdatedAt, &inv.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("investigation %s: %w", id, contracts.ErrNotFound)
		}
		return nil, fmt.Errorf("query investigation: %w", err)
	}
	if targetsRaw != nil {
		if err := json.Unmarshal(targetsRaw, &inv.Targets); err != nil {
			return nil, fmt.Errorf("unmarshal targets: %w", err)
		}
	}
	return &inv, nil
}

func (r *InvestigationRepo) UpdateStatus(ctx context.Context, id string, status contracts.InvestigationStatus) error {
	tag, err := r.q.Exec(ctx,
		`UPDATE investigations SET status = $1, updated_at = $2 WHERE id = $3`,
		status, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("investigation %s: %w", id, contracts.ErrNotFound)
	}
	return nil
}

func (r *InvestigationRepo) Complete(ctx context.Context, id string, headline string, confidence float64) error {
	now := time.Now().UTC()
	tag, err := r.q.Exec(ctx, `
		UPDATE investigations
		SET status = $1, headline = $2, confidence = $3,
		    updated_at = $4, completed_at = $5
		WHERE id = $6`,
		contracts.StatusDone, headline, confidence, now, now, id,
	)
	if err != nil {
		return fmt.Errorf("complete investigation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("investigation %s: %w", id, contracts.ErrNotFound)
	}
	return nil
}

func (r *InvestigationRepo) FindActiveByFingerprint(ctx context.Context, fingerprint string, since time.Time) (*contracts.ActiveInvestigation, error) {
	var inv contracts.ActiveInvestigation

	err := r.q.QueryRow(ctx, `
		SELECT i.id, i.status, i.headline, i.confidence,
		       i.slack_channel_id, i.slack_thread_ts,
		       i.created_at, i.completed_at
		FROM investigations i
		JOIN (SELECT unnest(alert_ids) AS alert_id, id AS inv_id FROM investigations) ia ON ia.inv_id = i.id
		JOIN alerts a ON a.id::text = ia.alert_id
		WHERE a.fingerprint = $1
		  AND i.created_at >= $2
		  AND i.status != $3
		ORDER BY i.created_at DESC
		LIMIT 1`,
		fingerprint, since, string(contracts.StatusFailed),
	).Scan(
		&inv.ID, &inv.Status, &inv.Headline, &inv.Confidence,
		&inv.SlackChannelID, &inv.SlackThreadTS,
		&inv.CreatedAt, &inv.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find active investigation by fingerprint: %w", err)
	}
	return &inv, nil
}

func (r *InvestigationRepo) LinkAlertToInvestigation(ctx context.Context, investigationID, alertID string) error {
	tag, err := r.q.Exec(ctx, `
		UPDATE investigations
		SET alert_ids = array_append(alert_ids, $1), updated_at = $2
		WHERE id = $3`,
		alertID, time.Now().UTC(), investigationID,
	)
	if err != nil {
		return fmt.Errorf("link alert to investigation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("investigation %s: %w", investigationID, contracts.ErrNotFound)
	}
	return nil
}
