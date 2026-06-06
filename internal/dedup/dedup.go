package dedup

import (
	"context"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type InvestigationLookup interface {
	FindActiveByFingerprint(ctx context.Context, fingerprint string, since time.Time) (*ActiveInvestigation, error)
	LinkAlertToInvestigation(ctx context.Context, investigationID, alertID string) error
}

type ActiveInvestigation struct {
	ID             string
	Status         string
	Headline       string
	Confidence     float64
	SlackChannelID string
	SlackThreadTS  string
	CreatedAt      time.Time
	CompletedAt    *time.Time
}

type Result struct {
	IsDuplicate      bool
	ExistingID       string
	ExistingHeadline string
	ExistingChannel  string
	ExistingThread   string
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

func (s *Service) Check(ctx context.Context, alert contracts.NormalizedAlert) (*Result, error) {
	if alert.Fingerprint == "" {
		return &Result{IsDuplicate: false}, nil
	}

	since := time.Now().UTC().Add(-s.window)
	existing, err := s.store.FindActiveByFingerprint(ctx, alert.Fingerprint, since)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return &Result{IsDuplicate: false}, nil
	}

	if err := s.store.LinkAlertToInvestigation(ctx, existing.ID, alert.ID); err != nil {
		s.logger.Warn("failed to link alert to existing investigation",
			zap.String("investigation_id", existing.ID),
			zap.String("alert_id", alert.ID),
			zap.Error(err),
		)
	}

	return &Result{
		IsDuplicate:      true,
		ExistingID:       existing.ID,
		ExistingHeadline: existing.Headline,
		ExistingChannel:  existing.SlackChannelID,
		ExistingThread:   existing.SlackThreadTS,
	}, nil
}
