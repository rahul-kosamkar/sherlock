package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type SuppressionRepo struct {
	q Querier
}

func NewSuppressionRepo(db *DB) *SuppressionRepo {
	return &SuppressionRepo{q: db.pool}
}

func (r *SuppressionRepo) WithTx(tx pgx.Tx) *SuppressionRepo {
	return &SuppressionRepo{q: tx}
}

func (r *SuppressionRepo) Create(ctx context.Context, fingerprint string, expiresAt time.Time, createdBy, reason string) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO suppressions (fingerprint, expires_at, created_by, reason)
		VALUES ($1, $2, $3, $4)`,
		fingerprint, expiresAt, createdBy, reason,
	)
	if err != nil {
		return fmt.Errorf("insert suppression: %w", err)
	}
	return nil
}

func (r *SuppressionRepo) IsActive(ctx context.Context, fingerprint string) (bool, error) {
	var count int
	err := r.q.QueryRow(ctx, `
		SELECT COUNT(*) FROM suppressions
		WHERE fingerprint = $1 AND expires_at > now()`,
		fingerprint,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check suppression: %w", err)
	}
	return count > 0, nil
}
