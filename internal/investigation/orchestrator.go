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
	"go.opentelemetry.io/otel/propagation"
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
	Subscribe(ctx context.Context, subject string, handler func(msg []byte, ack func(), nak func()) error) error
}

type QueuePublisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

type BlobStore interface {
	PutEvidenceBlob(ctx context.Context, key string, data []byte) error
}

type TxBeginner interface {
	BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type TxRepoFactory interface {
	InvestigationRepoTx(tx Tx) InvestigationStore
	EvidenceRepoTx(tx Tx) EvidenceStore
	TimelineRepoTx(tx Tx) TimelineStore
	HypothesisRepoTx(tx Tx) HypothesisStore
}

type OrchestratorDeps struct {
	Investigations InvestigationStore
	Evidence       EvidenceStore
	Timelines      TimelineStore
	Hypotheses     HypothesisStore
	Alerts         AlertStore
	Collectors     CollectorSet
	Correlator     CorrelatorService
	RCA            RCAService
	Timeline       TimelineBuilder
	Entity         EntityResolver
	Slack          SlackNotifier
	Audit          AuditLogger
	Consumer       QueueConsumer
	Logger         *zap.Logger
	TxBeginner     TxBeginner
	TxRepos        TxRepoFactory
}

type Option func(*Orchestrator)

func WithLLMEngine(engine RCAService) Option {
	return func(o *Orchestrator) { o.llmRCA = engine; o.llmEnabled = true }
}

func WithRemediation(r RemediationEvaluator) Option {
	return func(o *Orchestrator) { o.remediation = r }
}

func WithMaxConcurrent(n int) Option {
	return func(o *Orchestrator) { o.maxConcurrent = n; o.sem = make(chan struct{}, n) }
}

func WithTimeout(d time.Duration) Option {
	return func(o *Orchestrator) { o.timeout = d }
}

func WithBlobStore(bs BlobStore) Option {
	return func(o *Orchestrator) { o.blobStore = bs }
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
	blobStore      BlobStore
	txBeginner     TxBeginner
	txRepos        TxRepoFactory
	logger         *zap.Logger
	maxConcurrent  int
	timeout        time.Duration
	llmEnabled     bool

	sem chan struct{}
	wg  sync.WaitGroup
}

func NewOrchestrator(deps OrchestratorDeps, opts ...Option) *Orchestrator {
	o := &Orchestrator{
		investigations: deps.Investigations,
		evidence:       deps.Evidence,
		timelines:      deps.Timelines,
		hypotheses:     deps.Hypotheses,
		alerts:         deps.Alerts,
		collectors:     deps.Collectors,
		correlator:     deps.Correlator,
		rca:            deps.RCA,
		timeline:       deps.Timeline,
		entity:         deps.Entity,
		slack:          deps.Slack,
		audit:          deps.Audit,
		consumer:       deps.Consumer,
		txBeginner:     deps.TxBeginner,
		txRepos:        deps.TxRepos,
		logger:         deps.Logger,
		maxConcurrent:  10,
		timeout:        5 * time.Minute,
		sem:            make(chan struct{}, 10),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *Orchestrator) Start(ctx context.Context, streamName string) error {
	subject := streamName + ".new"
	return o.consumer.Subscribe(ctx, subject, func(msg []byte, ack func(), nak func()) error {
		var job contracts.InvestigationJob
		if err := json.Unmarshal(msg, &job); err != nil {
			o.logger.Error("failed to unmarshal investigation job", zap.Error(err))
			return fmt.Errorf("unmarshal job: %w", err)
		}

		o.sem <- struct{}{}
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			defer func() { <-o.sem }()
			defer func() {
				if r := recover(); r != nil {
					o.logger.Error("investigation panicked",
						zap.String("alert_id", job.Alert.ID),
						zap.Any("panic", r),
					)
					nak()
				}
			}()

			invCtx, cancel := context.WithTimeout(ctx, o.timeout)
			defer cancel()

			if err := o.runInvestigation(invCtx, job); err != nil {
				o.logger.Error("investigation failed",
					zap.String("alert_id", job.Alert.ID),
					zap.Error(err),
				)
				nak()
				return
			}
			ack()
		}()

		return nil
	})
}

func (o *Orchestrator) Stop() {
	o.wg.Wait()
}

func (o *Orchestrator) runInvestigation(ctx context.Context, job contracts.InvestigationJob) (retErr error) {
	invStart := time.Now()
	metrics.InvestigationsStarted.Inc()
	metrics.ActiveInvestigations.Inc()
	defer metrics.ActiveInvestigations.Dec()

	var investigationFailed bool
	defer func() {
		if !investigationFailed {
			if retErr != nil {
				metrics.InvestigationsCompleted.WithLabelValues("failed").Inc()
			} else {
				metrics.InvestigationsCompleted.WithLabelValues("done").Inc()
			}
		}
		metrics.InvestigationDuration.Observe(time.Since(invStart).Seconds())
	}()

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

	carrier := propagation.MapCarrier{}
	carrier.Set("traceparent", job.TraceParent)
	carrier.Set("tracestate", job.TraceState)
	parentCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)
	remoteSpanCtx := trace.SpanContextFromContext(parentCtx)
	var spanOpts []trace.SpanStartOption
	spanOpts = append(spanOpts, trace.WithAttributes(
		attribute.String("investigation.id", inv.ID),
		attribute.String("alert.id", job.Alert.ID),
	))
	if remoteSpanCtx.IsValid() {
		spanOpts = append(spanOpts, trace.WithLinks(trace.Link{SpanContext: remoteSpanCtx}))
	}
	ctx, span := tracer.Start(ctx, "investigation.run", spanOpts...)
	defer span.End()

	if err := o.alerts.Create(ctx, &job.Alert); err != nil {
		o.logger.Warn("failed to store alert (may already exist)", zap.Error(err))
	}

	if err := o.investigations.Create(ctx, inv); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create investigation failed")
		return fmt.Errorf("create investigation: %w", err)
	}

	_ = o.audit.Log(ctx, inv.TenantID, "system", "investigation.created", inv.ID, map[string]string{
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
		_ = o.slack.PostEvidenceUpdate(ctx, inv.SlackChannelID, inv.SlackThreadTS,
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

	if o.blobStore != nil {
		for i := range collected {
			if len(collected[i].BodyRef) > 4096 {
				blobKey := fmt.Sprintf("evidence/%s/%s", inv.ID, collected[i].ID)
				if err := o.blobStore.PutEvidenceBlob(ctx, blobKey, []byte(collected[i].BodyRef)); err != nil {
					o.logger.Warn("failed to offload evidence body to blob store",
						zap.String("evidence_id", collected[i].ID),
						zap.Error(err),
					)
				} else {
					collected[i].BodyRef = blobKey
				}
			}
		}
	}

	if err := o.storeEvidenceBatch(ctx, inv.ID, collected); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "store evidence failed")
		return fmt.Errorf("store evidence: %w", err)
	}

	if inv.SlackChannelID != "" && o.slack != nil {
		_ = o.slack.PostEvidenceUpdate(ctx, inv.SlackChannelID, inv.SlackThreadTS,
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
		investigationFailed = true
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

	events := o.timeline.Build(invData)

	headline := "No clear hypothesis identified"
	confidence := 0.0
	if len(hypotheses) > 0 {
		headline = hypotheses[0].Title
		confidence = hypotheses[0].Confidence
	}

	if err := o.storeCompletionBatch(ctx, inv.ID, headline, confidence, hypotheses, events); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "store completion batch failed")
		return fmt.Errorf("store completion batch: %w", err)
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

	span.SetAttributes(
		attribute.String("investigation.headline", headline),
		attribute.Float64("investigation.confidence", confidence),
		attribute.Int("evidence.count", len(collected)),
		attribute.Int("hypotheses.count", len(hypotheses)),
	)

	_ = o.audit.Log(ctx, inv.TenantID, "system", "investigation.completed", inv.ID, map[string]string{
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
	_ = o.investigations.UpdateStatus(ctx, inv.ID, contracts.StatusFailed)

	if inv.SlackChannelID != "" && o.slack != nil {
		_ = o.slack.PostError(ctx, inv.SlackChannelID, inv.SlackThreadTS, errMsg)
	}

	_ = o.audit.Log(ctx, inv.TenantID, "system", "investigation.failed", inv.ID, map[string]string{
		"error": errMsg,
	})

	return fmt.Errorf("investigation failed: %s", errMsg)
}

func (o *Orchestrator) storeEvidenceBatch(ctx context.Context, investigationID string, collected []contracts.Evidence) error {
	if len(collected) == 0 {
		return nil
	}
	if o.txBeginner != nil && o.txRepos != nil {
		tx, err := o.txBeginner.BeginTx(ctx)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if err := o.txRepos.EvidenceRepoTx(tx).CreateBatch(ctx, collected); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	return o.evidence.CreateBatch(ctx, collected)
}

func (o *Orchestrator) storeCompletionBatch(ctx context.Context, invID, headline string, confidence float64, hypotheses []contracts.Hypothesis, events []contracts.TimelineEvent) error {
	if o.txBeginner != nil && o.txRepos != nil {
		tx, err := o.txBeginner.BeginTx(ctx)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		txInv := o.txRepos.InvestigationRepoTx(tx)
		if len(hypotheses) > 0 {
			if err := o.txRepos.HypothesisRepoTx(tx).CreateBatch(ctx, invID, hypotheses); err != nil {
				return fmt.Errorf("store hypotheses: %w", err)
			}
		}
		if len(events) > 0 {
			if err := o.txRepos.TimelineRepoTx(tx).CreateBatch(ctx, events); err != nil {
				return fmt.Errorf("store timeline: %w", err)
			}
		}
		if err := txInv.UpdateStatus(ctx, invID, contracts.StatusPublishing); err != nil {
			return fmt.Errorf("update status to publishing: %w", err)
		}
		if err := txInv.Complete(ctx, invID, headline, confidence); err != nil {
			return fmt.Errorf("complete investigation: %w", err)
		}
		return tx.Commit(ctx)
	}

	if len(hypotheses) > 0 {
		if err := o.hypotheses.CreateBatch(ctx, invID, hypotheses); err != nil {
			return fmt.Errorf("store hypotheses: %w", err)
		}
	}
	if len(events) > 0 {
		if err := o.timelines.CreateBatch(ctx, events); err != nil {
			return fmt.Errorf("store timeline: %w", err)
		}
	}
	if err := o.investigations.UpdateStatus(ctx, invID, contracts.StatusPublishing); err != nil {
		return fmt.Errorf("update status to publishing: %w", err)
	}
	return o.investigations.Complete(ctx, invID, headline, confidence)
}

func EnqueueInvestigation(ctx context.Context, publisher QueuePublisher, streamName string, alert contracts.NormalizedAlert, channelID, threadTS, userID string) (string, error) {
	if alert.ID == "" {
		alert.ID = uuid.New().String()
	}

	job := contracts.InvestigationJob{
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
