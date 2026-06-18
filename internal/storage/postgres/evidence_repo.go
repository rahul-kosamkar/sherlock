package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type EvidenceRepo struct {
	q Querier
}

func NewEvidenceRepo(db *DB) *EvidenceRepo {
	return &EvidenceRepo{q: db.pool}
}

func (r *EvidenceRepo) WithTx(tx pgx.Tx) *EvidenceRepo {
	return &EvidenceRepo{q: tx}
}

func NewEvidenceRepoTx(tx pgx.Tx) *EvidenceRepo {
	return &EvidenceRepo{q: tx}
}

func (r *EvidenceRepo) CreateBatch(ctx context.Context, evidence []contracts.Evidence) error {
	batch := &pgx.Batch{}
	for _, e := range evidence {
		target, err := json.Marshal(e.Target)
		if err != nil {
			return fmt.Errorf("marshal target: %w", err)
		}
		attrs, err := json.Marshal(e.Attributes)
		if err != nil {
			return fmt.Errorf("marshal attributes: %w", err)
		}
		batch.Queue(`
			INSERT INTO evidence (
				id, investigation_id, kind, source, target,
				collected_at, observed_at_from, observed_at_to,
				summary, body_ref, query, score, attributes, redaction_state
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			e.ID, e.InvestigationID, e.Kind, e.Source, target,
			e.CollectedAt, e.ObservedAtFrom, e.ObservedAtTo,
			e.Summary, e.BodyRef, e.Query, e.Score, attrs, e.RedactionState,
		)
	}

	br := r.q.SendBatch(ctx, batch)
	defer br.Close()
	for range evidence {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert evidence: %w", err)
		}
	}
	return nil
}

func (r *EvidenceRepo) ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.Evidence, error) {
	rows, err := r.q.Query(ctx, `
		SELECT id, investigation_id, kind, source, target,
		       collected_at, observed_at_from, observed_at_to,
		       summary, body_ref, query, score, attributes, redaction_state
		FROM evidence
		WHERE investigation_id = $1`, investigationID)
	if err != nil {
		return nil, fmt.Errorf("query evidence: %w", err)
	}
	defer rows.Close()

	var result []contracts.Evidence
	for rows.Next() {
		var e contracts.Evidence
		var targetRaw, attrsRaw []byte
		if err := rows.Scan(
			&e.ID, &e.InvestigationID, &e.Kind, &e.Source, &targetRaw,
			&e.CollectedAt, &e.ObservedAtFrom, &e.ObservedAtTo,
			&e.Summary, &e.BodyRef, &e.Query, &e.Score, &attrsRaw, &e.RedactionState,
		); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		if targetRaw != nil {
			if err := json.Unmarshal(targetRaw, &e.Target); err != nil {
				return nil, fmt.Errorf("unmarshal target: %w", err)
			}
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
