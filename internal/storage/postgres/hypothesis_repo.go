package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type HypothesisRepo struct {
	db *DB
}

func NewHypothesisRepo(db *DB) *HypothesisRepo {
	return &HypothesisRepo{db: db}
}

func (r *HypothesisRepo) CreateBatch(ctx context.Context, investigationID string, hypotheses []contracts.Hypothesis) error {
	batch := &pgx.Batch{}
	for _, h := range hypotheses {
		fixes, err := json.Marshal(h.SuggestedFixes)
		if err != nil {
			return fmt.Errorf("marshal suggested_fixes: %w", err)
		}
		batch.Queue(`
			INSERT INTO hypotheses (
				id, investigation_id, title, narrative, cause_category,
				confidence, supporting, contradicting, suggested_fixes
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			h.ID, investigationID, h.Title, h.Narrative, h.CauseCategory,
			h.Confidence, h.Supporting, h.Contradicting, fixes,
		)
	}

	br := r.db.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range hypotheses {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert hypothesis: %w", err)
		}
	}
	return nil
}

func (r *HypothesisRepo) ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.Hypothesis, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT id, title, narrative, cause_category, confidence,
		       supporting, contradicting, suggested_fixes
		FROM hypotheses
		WHERE investigation_id = $1
		ORDER BY confidence DESC`, investigationID)
	if err != nil {
		return nil, fmt.Errorf("query hypotheses: %w", err)
	}
	defer rows.Close()

	var result []contracts.Hypothesis
	for rows.Next() {
		var h contracts.Hypothesis
		var fixesRaw []byte
		if err := rows.Scan(
			&h.ID, &h.Title, &h.Narrative, &h.CauseCategory, &h.Confidence,
			&h.Supporting, &h.Contradicting, &fixesRaw,
		); err != nil {
			return nil, fmt.Errorf("scan hypothesis: %w", err)
		}
		if fixesRaw != nil {
			if err := json.Unmarshal(fixesRaw, &h.SuggestedFixes); err != nil {
				return nil, fmt.Errorf("unmarshal suggested_fixes: %w", err)
			}
		}
		result = append(result, h)
	}
	return result, rows.Err()
}
