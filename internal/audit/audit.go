package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type AuditStore interface {
	Create(ctx context.Context, entry *contracts.AuditEntry) error
}

type Logger struct {
	store  AuditStore
	logger *zap.Logger
}

func NewLogger(store AuditStore, zapLogger *zap.Logger) *Logger {
	return &Logger{
		store:  store,
		logger: zapLogger,
	}
}

func (l *Logger) Log(ctx context.Context, tenantID, actor, action, target string, metadata map[string]string) error {
	entry := &contracts.AuditEntry{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		Actor:     actor,
		Action:    action,
		Target:    target,
		Metadata:  metadata,
		Timestamp: time.Now().UTC(),
	}
	return l.LogAction(ctx, entry)
}

func (l *Logger) LogAction(ctx context.Context, entry *contracts.AuditEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	if err := l.store.Create(ctx, entry); err != nil {
		return fmt.Errorf("storing audit entry: %w", err)
	}

	l.logger.Info("audit",
		zap.String("id", entry.ID),
		zap.String("tenant_id", entry.TenantID),
		zap.String("actor", entry.Actor),
		zap.String("action", entry.Action),
		zap.String("target", entry.Target),
		zap.Time("timestamp", entry.Timestamp),
	)

	return nil
}
