package receiver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/metrics"
)

const maxBodySize = 1 << 20 // 1 MB

type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

type BlobStore interface {
	PutRawPayload(ctx context.Context, key string, data []byte) error
}

type DedupChecker interface {
	Check(ctx context.Context, alert contracts.NormalizedAlert) (*contracts.DedupResult, error)
}

type DedupNotifier interface {
	PostDedupNotification(ctx context.Context, channelID, threadTS, existingID, alertValue string) error
}

type SuppressChecker interface {
	IsActive(ctx context.Context, fingerprint string) (bool, error)
}

type Gateway struct {
	receivers     map[string]contracts.AlertReceiver
	publisher     Publisher
	blobStore     BlobStore
	dedup         DedupChecker
	dedupNotifier DedupNotifier
	suppress      SuppressChecker
	rateLimiter   func(http.Handler) http.Handler
	logger        *zap.Logger
	streamName    string
}

func NewGateway(publisher Publisher, blobStore BlobStore, logger *zap.Logger) *Gateway {
	return &Gateway{
		receivers:  make(map[string]contracts.AlertReceiver),
		publisher:  publisher,
		blobStore:  blobStore,
		logger:     logger,
		streamName: "INVESTIGATIONS",
	}
}

func (g *Gateway) Register(receiver contracts.AlertReceiver) {
	g.receivers[receiver.Source()] = receiver
}

func (g *Gateway) SetDedup(d DedupChecker) {
	g.dedup = d
}

func (g *Gateway) SetDedupNotifier(n DedupNotifier) {
	g.dedupNotifier = n
}

func (g *Gateway) SetSuppress(s SuppressChecker) {
	g.suppress = s
}

func (g *Gateway) SetRateLimiter(rl func(http.Handler) http.Handler) {
	g.rateLimiter = rl
}

func (g *Gateway) Routes() chi.Router {
	r := chi.NewRouter()
	if g.rateLimiter != nil {
		r.Use(g.rateLimiter)
	}
	r.Post("/webhooks/{source}", g.handleWebhook)
	return r
}

func (g *Gateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	source := chi.URLParam(r, "source")
	ctx := r.Context()

	tracer := otel.Tracer("sherlock.receiver")
	ctx, span := tracer.Start(ctx, "webhook.receive",
		trace.WithAttributes(attribute.String("source", source)))
	defer span.End()
	r = r.WithContext(ctx)

	receiver, ok := g.receivers[source]
	if !ok {
		g.logger.Warn("unknown alert source", zap.String("source", source))
		span.SetStatus(codes.Error, "unknown source")
		http.Error(w, "unknown source", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		g.logger.Error("failed to read body", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, "body read failed")
		http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}

	span.AddEvent("webhook.verify")
	if err := receiver.Verify(ctx, r.Header, body); err != nil {
		g.logger.Warn("verification failed", zap.String("source", source), zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, "verification failed")
		metrics.WebhooksReceived.WithLabelValues(source, "rejected").Inc()
		http.Error(w, "verification failed", http.StatusUnauthorized)
		return
	}

	blobKey := fmt.Sprintf("raw/%s/%s/%s", source, time.Now().UTC().Format("2006-01-02"), uuid.NewString())
	if err := g.blobStore.PutRawPayload(ctx, blobKey, body); err != nil {
		g.logger.Error("failed to store raw payload", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, "blob store failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	span.AddEvent("webhook.decode")
	alerts, err := receiver.Decode(ctx, r.Header, body)
	if err != nil {
		g.logger.Error("failed to decode alerts", zap.String("source", source), zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, "decode failed")
		metrics.WebhooksReceived.WithLabelValues(source, "decode_error").Inc()
		http.Error(w, "decode error", http.StatusBadRequest)
		return
	}
	span.SetAttributes(attribute.Int("alerts.count", len(alerts)))

	subject := g.streamName + ".new"
	dedupCount := 0
	for i := range alerts {
		alerts[i].ID = uuid.NewString()
		alerts[i].RawRef = blobKey

		if g.dedup != nil {
			span.AddEvent("webhook.dedup_check", trace.WithAttributes(
				attribute.String("alert_id", alerts[i].ID)))
			result, err := g.dedup.Check(ctx, alerts[i])
			if err != nil {
				g.logger.Error("dedup check failed, proceeding with investigation",
					zap.String("alert_id", alerts[i].ID),
					zap.Error(err),
				)
				span.RecordError(err)
			} else if result != nil && result.IsDuplicate {
				g.logger.Info("duplicate alert detected, skipping investigation",
					zap.String("alert_id", alerts[i].ID),
					zap.String("fingerprint", alerts[i].Fingerprint),
					zap.String("existing_investigation", result.ExistingID),
				)
				metrics.DedupHits.Inc()
				if g.dedupNotifier != nil && result.ExistingChannel != "" {
					alertValue := alerts[i].Title
					if alertValue == "" {
						alertValue = alerts[i].ID
					}
					if err := g.dedupNotifier.PostDedupNotification(ctx, result.ExistingChannel, result.ExistingThread, result.ExistingID, alertValue); err != nil {
						g.logger.Warn("failed to post dedup notification to slack", zap.Error(err))
					}
				}
				dedupCount++
				continue
			}
		}

		if g.suppress != nil && alerts[i].Fingerprint != "" {
			suppressed, err := g.suppress.IsActive(ctx, alerts[i].Fingerprint)
			if err != nil {
				g.logger.Error("suppression check failed, proceeding with investigation",
					zap.String("alert_id", alerts[i].ID),
					zap.Error(err),
				)
			} else if suppressed {
				g.logger.Info("alert suppressed, skipping investigation",
					zap.String("alert_id", alerts[i].ID),
					zap.String("fingerprint", alerts[i].Fingerprint),
				)
				metrics.SuppressHits.Inc()
				dedupCount++
				continue
			}
		}

		job := contracts.InvestigationJob{Alert: alerts[i]}
		traceCarrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(r.Context(), traceCarrier)
		job.TraceParent = traceCarrier.Get("traceparent")
		job.TraceState = traceCarrier.Get("tracestate")
		data, err := json.Marshal(job)
		if err != nil {
			g.logger.Error("failed to marshal investigation job", zap.Error(err))
			span.RecordError(err)
			continue
		}
		span.AddEvent("webhook.publish", trace.WithAttributes(
			attribute.String("alert_id", alerts[i].ID)))
		if err := g.publisher.Publish(ctx, subject, data); err != nil {
			g.logger.Error("failed to publish investigation job", zap.Error(err))
			span.RecordError(err)
			span.SetStatus(codes.Error, "publish failed")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	metrics.WebhooksReceived.WithLabelValues(source, "accepted").Inc()
	metrics.WebhookDuration.WithLabelValues(source).Observe(time.Since(start).Seconds())

	if dedupCount > 0 && dedupCount == len(alerts) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		resp := fmt.Sprintf(`{"status":"deduplicated","message":"%d alert(s) linked to existing investigations"}`, dedupCount)
		_, _ = w.Write([]byte(resp))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
