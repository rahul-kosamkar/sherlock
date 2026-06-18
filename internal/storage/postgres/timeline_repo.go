package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type TimelineRepo struct {
	q Querier
}

func NewTimelineRepo(db *DB) *TimelineRepo {
	return &TimelineRepo{q: db.pool}
}

func (r *TimelineRepo) WithTx(tx pgx.Tx) *TimelineRepo {
	return &TimelineRepo{q: tx}
}

func NewTimelineRepoTx(tx pgx.Tx) *TimelineRepo {
	return &TimelineRepo{q: tx}
}

func (r *TimelineRepo) CreateBatch(ctx context.Context, events []contracts.TimelineEvent) error {
	batch := &pgx.Batch{}
	for _, e := range events {
		attrs, err := json.Marshal(e.Attributes)
		if err != nil {
			return fmt.Errorf("marshal attributes: %w", err)
		}
		batch.Queue(`
			INSERT INTO timeline_events (
				id, investigation_id, timestamp, kind, source,
				narrative, evidence_ids, attributes
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			e.ID, e.InvestigationID, e.Timestamp, e.Kind, e.Source,
			e.Narrative, e.EvidenceIDs, attrs,
		)
	}

	br := r.q.SendBatch(ctx, batch)
	defer br.Close()
	for range events {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert timeline event: %w", err)
		}
	}
	return nil
}

func (r *TimelineRepo) ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.TimelineEvent, error) {
	rows, err := r.q.Query(ctx, `
		SELECT id, investigation_id, timestamp, kind, source,
		       narrative, evidence_ids, attributes
		FROM timeline_events
		WHERE investigation_id = $1
		ORDER BY timestamp ASC`, investigationID)
	if err != nil {
		return nil, fmt.Errorf("query timeline events: %w", err)
	}
	defer rows.Close()

	var result []contracts.TimelineEvent
	for rows.Next() {
		var e contracts.TimelineEvent
		var attrsRaw []byte
		if err := rows.Scan(
			&e.ID, &e.InvestigationID, &e.Timestamp, &e.Kind, &e.Source,
			&e.Narrative, &e.EvidenceIDs, &attrsRaw,
		); err != nil {
			return nil, fmt.Errorf("scan timeline event: %w", err)
		}
		if attrsRaw != nil {
			if err := json.Unmarshal(attrsRaw, &e.Attributes); err != nil {
				return nil, fmt.Errorf("unmarshal attributes: %w", err)
			}
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
