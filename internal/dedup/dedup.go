package dedup

import (
	"context"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type InvestigationLookup interface {
	FindActiveByFingerprint(ctx context.Context, fingerprint string, since time.Time) (*contracts.ActiveInvestigation, error)
	LinkAlertToInvestigation(ctx context.Context, investigationID, alertID string) error
}

type Service struct {
	store  InvestigationLookup
	window time.Duration
	logger *zap.Logger
}

func New(store InvestigationLookup, window time.Duration, logger *zap.Logger) *Service {
	return &Service{
		store:  store,
		window: window,
		logger: logger,
	}
}

func (s *Service) Check(ctx context.Context, alert contracts.NormalizedAlert) (*contracts.DedupResult, error) {
	if alert.Fingerprint == "" {
		return &contracts.DedupResult{IsDuplicate: false}, nil
	}

	since := time.Now().UTC().Add(-s.window)
	existing, err := s.store.FindActiveByFingerprint(ctx, alert.Fingerprint, since)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return &contracts.DedupResult{IsDuplicate: false}, nil
	}

	if err := s.store.LinkAlertToInvestigation(ctx, existing.ID, alert.ID); err != nil {
		s.logger.Warn("failed to link alert to existing investigation",
			zap.String("investigation_id", existing.ID),
			zap.String("alert_id", alert.ID),
			zap.Error(err),
		)
	}

	return &contracts.DedupResult{
		IsDuplicate:      true,
		ExistingID:       existing.ID,
		ExistingHeadline: existing.Headline,
		ExistingChannel:  existing.SlackChannelID,
		ExistingThread:   existing.SlackThreadTS,
	}, nil
}
