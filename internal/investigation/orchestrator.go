package investigation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type EvidenceStore interface {
	CreateBatch(ctx context.Context, evidence []contracts.Evidence) error
	ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.Evidence, error)
}

type InvestigationStore interface {
	Create(ctx context.Context, inv *contracts.Investigation) error
	GetByID(ctx context.Context, id string) (*contracts.Investigation, error)
	UpdateStatus(ctx context.Context, id string, status contracts.InvestigationStatus) error
	Complete(ctx context.Context, id string, headline string, confidence float64) error
}

type TimelineStore interface {
	CreateBatch(ctx context.Context, events []contracts.TimelineEvent) error
}

type HypothesisStore interface {
	CreateBatch(ctx context.Context, investigationID string, hypotheses []contracts.Hypothesis) error
}

type AlertStore interface {
	Create(ctx context.Context, alert *contracts.NormalizedAlert) error
	GetByID(ctx context.Context, id string) (*contracts.NormalizedAlert, error)
}

type CollectorSet interface {
	CollectAll(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error)
}

type CorrelatorService interface {
	Correlate(ctx context.Context, data contracts.InvestigationData) (contracts.InvestigationGraph, error)
}

type RCAService interface {
	Name() string
	Rank(ctx context.Context, graph contracts.InvestigationGraph) ([]contracts.Hypothesis, error)
}

type TimelineBuilder interface {
	Build(data contracts.InvestigationData) []contracts.TimelineEvent
}

type EntityResolver interface {
	Resolve(alert *contracts.NormalizedAlert) EntityResolveResult
}

type EntityResolveResult struct {
	Targets  []contracts.TargetRef
	TimeFrom time.Time
	TimeTo   time.Time
}

type SlackNotifier interface {
	PostInvestigationStarted(ctx context.Context, channelID string, investigationID string) (string, error)
	PostEvidenceUpdate(ctx context.Context, channelID, threadTS string, message string) error
	PostResult(ctx context.Context, channelID, threadTS string, result *contracts.InvestigationResult) error
	PostError(ctx context.Context, channelID, threadTS string, errMsg string) error
}

type AuditLogger interface {
	Log(ctx context.Context, tenantID, actor, action, target string, metadata map[string]string) error
}

type RemediationEvaluator interface {
	Evaluate(hypotheses []contracts.Hypothesis) []contracts.SuggestedFix
}

type QueueConsumer interface {
	Subscribe(ctx context.Context, subject string, handler func(msg []byte) error) error
}

type QueuePublisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

type Orchestrator struct {
	investigations InvestigationStore
	evidence       EvidenceStore
	timelines      TimelineStore
	hypotheses     HypothesisStore
	alerts         AlertStore
	collectors     CollectorSet
	correlator     CorrelatorService
	rca            RCAService
	llmRCA         RCAService
	timeline       TimelineBuilder
	entity         EntityResolver
	slack          SlackNotifier
	audit          AuditLogger
	remediation    RemediationEvaluator
	consumer       QueueConsumer
	logger         *zap.Logger
	maxConcurrent  int
	timeout        time.Duration
	llmEnabled     bool

	sem chan struct{}
	wg  sync.WaitGroup
}

type OrchestratorConfig struct {
	MaxConcurrent int
	Timeout       time.Duration
	StreamName    string
	LLMEnabled    bool
}

func NewOrchestrator(
	cfg OrchestratorConfig,
	investigations InvestigationStore,
	evidence EvidenceStore,
	timelines TimelineStore,
	hypotheses HypothesisStore,
	alerts AlertStore,
	collectors CollectorSet,
	correlator CorrelatorService,
	rca RCAService,
	timeline TimelineBuilder,
	entity EntityResolver,
	slack SlackNotifier,
	audit AuditLogger,
	consumer QueueConsumer,
	logger *zap.Logger,
) *Orchestrator {
	maxC := cfg.MaxConcurrent
	if maxC <= 0 {
		maxC = 10
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	return &Orchestrator{
		investigations: investigations,
		evidence:       evidence,
		timelines:      timelines,
		hypotheses:     hypotheses,
		alerts:         alerts,
		collectors:     collectors,
		correlator:     correlator,
		rca:            rca,
		timeline:       timeline,
		entity:         entity,
		slack:          slack,
		audit:          audit,
		consumer:       consumer,
		logger:         logger,
		maxConcurrent:  maxC,
		timeout:        timeout,
		llmEnabled:     cfg.LLMEnabled,
		sem:            make(chan struct{}, maxC),
	}
}

func (o *Orchestrator) SetLLMEngine(engine RCAService) {
	o.llmRCA = engine
}

func (o *Orchestrator) SetRemediation(r RemediationEvaluator) {
	o.remediation = r
}

type investigationJob struct {
	Alert          contracts.NormalizedAlert `json:"alert"`
	SlackChannelID string                   `json:"slack_channel_id,omitempty"`
	SlackThreadTS  string                   `json:"slack_thread_ts,omitempty"`
	RequestedBy    string                   `json:"requested_by,omitempty"`
}

func (o *Orchestrator) Start(ctx context.Context, streamName string) error {
	subject := streamName + ".new"
	return o.consumer.Subscribe(ctx, subject, func(msg []byte) error {
		var job investigationJob
		if err := json.Unmarshal(msg, &job); err != nil {
			o.logger.Error("failed to unmarshal investigation job", zap.Error(err))
			return fmt.Errorf("unmarshal job: %w", err)
		}

		o.sem <- struct{}{}
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			defer func() { <-o.sem }()

			invCtx, cancel := context.WithTimeout(ctx, o.timeout)
			defer cancel()

			if err := o.runInvestigation(invCtx, job); err != nil {
				o.logger.Error("investigation failed",
					zap.String("alert_id", job.Alert.ID),
					zap.Error(err),
				)
			}
		}()

		return nil
	})
}

func (o *Orchestrator) Stop() {
	o.wg.Wait()
}

func (o *Orchestrator) runInvestigation(ctx context.Context, job investigationJob) error {
	invStart := time.Now()
	metrics.InvestigationsStarted.Inc()
	metrics.ActiveInvestigations.Inc()
	defer metrics.ActiveInvestigations.Dec()

	inv := &contracts.Investigation{
		ID:             uuid.New().String(),
		TenantID:       job.Alert.TenantID,
		Status:         contracts.StatusPending,
		AlertIDs:       []string{job.Alert.ID},
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		SlackChannelID: job.SlackChannelID,
	}

	tracer := otel.Tracer("sherlock.investigation")
	ctx, span := tracer.Start(ctx, "investigation.run",
		trace.WithAttributes(
			attribute.String("investigation.id", inv.ID),
			attribute.String("alert.id", job.Alert.ID),
		))
	defer span.End()

	if err := o.alerts.Create(ctx, &job.Alert); err != nil {
		o.logger.Warn("failed to store alert (may already exist)", zap.Error(err))
	}

	if err := o.investigations.Create(ctx, inv); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create investigation failed")
		return fmt.Errorf("create investigation: %w", err)
	}

	o.audit.Log(ctx, inv.TenantID, "system", "investigation.created", inv.ID, map[string]string{
		"alert_id": job.Alert.ID,
	})

	if inv.SlackChannelID != "" && o.slack != nil {
		threadTS, err := o.slack.PostInvestigationStarted(ctx, inv.SlackChannelID, inv.ID)
		if err != nil {
			o.logger.Warn("failed to post investigation started", zap.Error(err))
		} else {
			inv.SlackThreadTS = threadTS
		}
	}

	if err := o.investigations.UpdateStatus(ctx, inv.ID, contracts.StatusCollecting); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "update status failed")
		return fmt.Errorf("update status to collecting: %w", err)
	}

	resolved := o.entity.Resolve(&job.Alert)
	inv.Targets = resolved.Targets
	inv.TimeFrom = resolved.TimeFrom
	inv.TimeTo = resolved.TimeTo

	collectReq := contracts.CollectRequest{
		InvestigationID: inv.ID,
		Alert:           job.Alert,
		Targets:         resolved.Targets,
		TimeFrom:        resolved.TimeFrom,
		TimeTo:          resolved.TimeTo,
	}

	if inv.SlackChannelID != "" && o.slack != nil {
		o.slack.PostEvidenceUpdate(ctx, inv.SlackChannelID, inv.SlackThreadTS,
			fmt.Sprintf("Collecting evidence for %d target(s)...", len(resolved.Targets)))
	}

	_, collectSpan := tracer.Start(ctx, "investigation.collect",
		trace.WithAttributes(attribute.Int("targets.count", len(resolved.Targets))))
	collected, err := o.collectors.CollectAll(ctx, collectReq)
	if err != nil {
		o.logger.Warn("partial collection failure", zap.Error(err))
		collectSpan.RecordError(err)
	}
	collectSpan.SetAttributes(attribute.Int("evidence.count", len(collected)))
	collectSpan.End()

	if len(collected) > 0 {
		if err := o.evidence.CreateBatch(ctx, collected); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "store evidence failed")
			return fmt.Errorf("store evidence: %w", err)
		}
	}

	if inv.SlackChannelID != "" && o.slack != nil {
		o.slack.PostEvidenceUpdate(ctx, inv.SlackChannelID, inv.SlackThreadTS,
			fmt.Sprintf("Collected %d evidence items. Analyzing...", len(collected)))
	}

	if err := o.investigations.UpdateStatus(ctx, inv.ID, contracts.StatusAnalyzing); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "update status failed")
		return fmt.Errorf("update status to analyzing: %w", err)
	}

	invData := contracts.InvestigationData{
		Investigation: *inv,
		Alerts:        []contracts.NormalizedAlert{job.Alert},
		Evidence:      collected,
	}

	_, corrSpan := tracer.Start(ctx, "investigation.correlate")
	graph, err := o.correlator.Correlate(ctx, invData)
	if err != nil {
		o.logger.Warn("correlation failed, proceeding without correlations", zap.Error(err))
		corrSpan.RecordError(err)
		graph = contracts.InvestigationGraph{Data: invData}
	}
	corrSpan.End()

	rcaEngine := o.selectRCAEngine()
	o.logger.Info("running RCA engine", zap.String("engine", rcaEngine.Name()))

	_, rcaSpan := tracer.Start(ctx, "investigation.rca",
		trace.WithAttributes(attribute.String("rca.engine", rcaEngine.Name())))
	hypotheses, err := rcaEngine.Rank(ctx, graph)
	if err != nil {
		o.logger.Error("RCA failed", zap.Error(err))
		rcaSpan.RecordError(err)
		rcaSpan.SetStatus(codes.Error, "RCA engine failed")
		rcaSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "RCA engine failed")
		return o.failInvestigation(ctx, inv, "RCA engine failed: "+err.Error())
	}
	rcaSpan.SetAttributes(attribute.Int("hypotheses.count", len(hypotheses)))
	rcaSpan.End()

	if o.remediation != nil && len(hypotheses) > 0 {
		policyFixes := o.remediation.Evaluate(hypotheses)
		if len(policyFixes) > 0 {
			existing := make(map[string]struct{})
			for _, fix := range hypotheses[0].SuggestedFixes {
				existing[fix.Title] = struct{}{}
			}
			for _, fix := range policyFixes {
				if _, dup := existing[fix.Title]; !dup {
					hypotheses[0].SuggestedFixes = append(hypotheses[0].SuggestedFixes, fix)
					existing[fix.Title] = struct{}{}
				}
			}
			o.logger.Info("remediation policies applied",
				zap.Int("policy_fixes", len(policyFixes)),
				zap.Int("total_fixes", len(hypotheses[0].SuggestedFixes)),
			)
		}
	}

	if len(hypotheses) > 0 {
		if err := o.hypotheses.CreateBatch(ctx, inv.ID, hypotheses); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "store hypotheses failed")
			return fmt.Errorf("store hypotheses: %w", err)
		}
	}

	events := o.timeline.Build(invData)
	if len(events) > 0 {
		if err := o.timelines.CreateBatch(ctx, events); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "store timeline failed")
			return fmt.Errorf("store timeline: %w", err)
		}
	}

	if err := o.investigations.UpdateStatus(ctx, inv.ID, contracts.StatusPublishing); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "update status failed")
		return fmt.Errorf("update status to publishing: %w", err)
	}

	headline := "No clear hypothesis identified"
	confidence := 0.0
	if len(hypotheses) > 0 {
		headline = hypotheses[0].Title
		confidence = hypotheses[0].Confidence
	}

	result := &contracts.InvestigationResult{
		InvestigationID: inv.ID,
		Status:          contracts.StatusDone,
		Headline:        headline,
		Confidence:      confidence,
		TopHypotheses:   hypotheses,
		RCAEngine:       rcaEngine.Name(),
	}
	for _, e := range events {
		result.TimelineEventIDs = append(result.TimelineEventIDs, e.ID)
	}
	if len(hypotheses) > 0 {
		result.RecommendedActions = hypotheses[0].SuggestedFixes
		if hypotheses[0].Narrative != "" {
			result.RootCause = hypotheses[0].Narrative
		}
	}

	_, pubSpan := tracer.Start(ctx, "investigation.publish")
	if inv.SlackChannelID != "" && o.slack != nil {
		if err := o.slack.PostResult(ctx, inv.SlackChannelID, inv.SlackThreadTS, result); err != nil {
			o.logger.Error("failed to post result to Slack", zap.Error(err))
			pubSpan.RecordError(err)
		}
	}
	pubSpan.End()

	if err := o.investigations.Complete(ctx, inv.ID, headline, confidence); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "complete investigation failed")
		return fmt.Errorf("complete investigation: %w", err)
	}

	span.SetAttributes(
		attribute.String("investigation.headline", headline),
		attribute.Float64("investigation.confidence", confidence),
		attribute.Int("evidence.count", len(collected)),
		attribute.Int("hypotheses.count", len(hypotheses)),
	)

	metrics.InvestigationsCompleted.WithLabelValues("done").Inc()
	metrics.InvestigationDuration.Observe(time.Since(invStart).Seconds())

	o.audit.Log(ctx, inv.TenantID, "system", "investigation.completed", inv.ID, map[string]string{
		"headline":   headline,
		"confidence": fmt.Sprintf("%.2f", confidence),
		"evidence":   fmt.Sprintf("%d", len(collected)),
		"hypotheses": fmt.Sprintf("%d", len(hypotheses)),
	})

	return nil
}

func (o *Orchestrator) selectRCAEngine() RCAService {
	if o.llmEnabled && o.llmRCA != nil {
		return o.llmRCA
	}
	return o.rca
}

func (o *Orchestrator) failInvestigation(ctx context.Context, inv *contracts.Investigation, errMsg string) error {
	metrics.InvestigationsCompleted.WithLabelValues("failed").Inc()
	o.investigations.UpdateStatus(ctx, inv.ID, contracts.StatusFailed)

	if inv.SlackChannelID != "" && o.slack != nil {
		o.slack.PostError(ctx, inv.SlackChannelID, inv.SlackThreadTS, errMsg)
	}

	o.audit.Log(ctx, inv.TenantID, "system", "investigation.failed", inv.ID, map[string]string{
		"error": errMsg,
	})

	return fmt.Errorf("investigation failed: %s", errMsg)
}

// EnqueueInvestigation creates and publishes an investigation job.
// Used by the Slack app to trigger investigations from commands/shortcuts.
func EnqueueInvestigation(ctx context.Context, publisher QueuePublisher, streamName string, alert contracts.NormalizedAlert, channelID, threadTS, userID string) (string, error) {
	if alert.ID == "" {
		alert.ID = uuid.New().String()
	}

	job := investigationJob{
		Alert:          alert,
		SlackChannelID: channelID,
		SlackThreadTS:  threadTS,
		RequestedBy:    userID,
	}

	data, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("marshal job: %w", err)
	}

	subject := streamName + ".new"
	if err := publisher.Publish(ctx, subject, data); err != nil {
		return "", fmt.Errorf("publish job: %w", err)
	}

	return alert.ID, nil
}
