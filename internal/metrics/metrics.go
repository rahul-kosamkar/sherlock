package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WebhooksReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sherlock",
		Name:      "webhooks_received_total",
		Help:      "Total webhooks received by source and status.",
	}, []string{"source", "status"})

	WebhookDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sherlock",
		Name:      "webhook_duration_seconds",
		Help:      "Webhook processing duration in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"source"})

	InvestigationsStarted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sherlock",
		Name:      "investigations_started_total",
		Help:      "Total investigations started.",
	})

	InvestigationsCompleted = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sherlock",
		Name:      "investigations_completed_total",
		Help:      "Total investigations completed by status.",
	}, []string{"status"})

	InvestigationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "sherlock",
		Name:      "investigation_duration_seconds",
		Help:      "Investigation processing duration in seconds.",
		Buckets:   []float64{1, 5, 10, 30, 60, 120, 300},
	})

	LLMCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sherlock",
		Name:      "llm_calls_total",
		Help:      "Total LLM calls by provider and status.",
	}, []string{"provider", "status"})

	LLMCallDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sherlock",
		Name:      "llm_call_duration_seconds",
		Help:      "LLM call duration in seconds.",
		Buckets:   []float64{1, 5, 10, 30, 60, 120},
	}, []string{"provider"})

	DedupHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sherlock",
		Name:      "dedup_hits_total",
		Help:      "Total duplicate alerts detected.",
	})

	SuppressHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sherlock",
		Name:      "suppress_hits_total",
		Help:      "Total suppressed alerts.",
	})

	EvidenceCollected = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sherlock",
		Name:      "evidence_collected_total",
		Help:      "Total evidence items collected by collector.",
	}, []string{"collector"})

	ActiveInvestigations = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sherlock",
		Name:      "active_investigations",
		Help:      "Number of currently running investigations.",
	})
)
