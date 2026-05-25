package prometheus

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

const (
	sourceName     = "prometheus"
	queryStep      = time.Minute
	anomalyFactor  = 2.0
	scoreAnomaly   = 0.8
	scoreElevated  = 0.5
	scoreNormal    = 0.2
)

type Collector struct {
	api promv1.API
	log *zap.Logger
}

func New(url string, log *zap.Logger) (*Collector, error) {
	client, err := promapi.NewClient(promapi.Config{Address: url})
	if err != nil {
		return nil, fmt.Errorf("prometheus client: %w", err)
	}
	return &Collector{
		api: promv1.NewAPI(client),
		log: log,
	}, nil
}

func (c *Collector) Name() string { return sourceName }

func (c *Collector) Collect(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	var evidence []contracts.Evidence

	for _, target := range req.Targets {
		queries := buildQueries(target, req.Alert.Labels)
		for _, q := range queries {
			ev, err := c.executeQuery(ctx, req, target, q)
			if err != nil {
				c.log.Warn("prometheus query failed",
					zap.String("query", q.expr),
					zap.Error(err),
				)
				continue
			}
			if ev != nil {
				evidence = append(evidence, *ev)
			}
		}
	}

	return evidence, nil
}

type metricQuery struct {
	name string
	expr string
}

func buildQueries(target contracts.TargetRef, labels map[string]string) []metricQuery {
	ns := target.Namespace
	name := target.Name

	queries := []metricQuery{
		{
			name: "cpu_usage",
			expr: fmt.Sprintf(`rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[5m])`, ns, name),
		},
		{
			name: "memory_usage",
			expr: fmt.Sprintf(`container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}`, ns, name),
		},
		{
			name: "restart_count",
			expr: fmt.Sprintf(`kube_pod_container_status_restarts_total{namespace="%s",pod=~"%s.*"}`, ns, name),
		},
	}

	if svc := firstLabel(labels, "service", "job"); svc != "" {
		queries = append(queries, metricQuery{
			name: "error_rate",
			expr: fmt.Sprintf(`rate(http_requests_total{service="%s",code=~"5.."}[5m])`, svc),
		})
	}

	return queries
}

func (c *Collector) executeQuery(ctx context.Context, req contracts.CollectRequest, target contracts.TargetRef, q metricQuery) (*contracts.Evidence, error) {
	r := promv1.Range{
		Start: req.TimeFrom,
		End:   req.TimeTo,
		Step:  queryStep,
	}

	result, warnings, err := c.api.QueryRange(ctx, q.expr, r)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", q.name, err)
	}
	for _, w := range warnings {
		c.log.Debug("prometheus warning", zap.String("query", q.name), zap.String("warning", w))
	}

	matrix, ok := result.(model.Matrix)
	if !ok || len(matrix) == 0 {
		return nil, nil
	}

	summary, score := analyzeMatrix(q.name, matrix)
	if score < scoreNormal {
		return nil, nil
	}

	return &contracts.Evidence{
		ID:              uuid.NewString(),
		InvestigationID: req.InvestigationID,
		Kind:            contracts.EvidenceMetric,
		Source:          sourceName,
		Target:          target,
		CollectedAt:     time.Now().UTC(),
		ObservedAtFrom:  req.TimeFrom,
		ObservedAtTo:    req.TimeTo,
		Summary:         summary,
		Query:           q.expr,
		Score:           score,
		Attributes: map[string]string{
			"metric": q.name,
		},
		RedactionState: contracts.RedactionNone,
	}, nil
}

func analyzeMatrix(name string, matrix model.Matrix) (string, float64) {
	for _, stream := range matrix {
		if len(stream.Values) < 2 {
			continue
		}

		var sum float64
		for _, sp := range stream.Values {
			sum += float64(sp.Value)
		}
		avg := sum / float64(len(stream.Values))
		last := float64(stream.Values[len(stream.Values)-1].Value)

		if avg == 0 {
			if last > 0 {
				return fmt.Sprintf("%s spiked from zero to %.4f", name, last), scoreAnomaly
			}
			continue
		}

		ratio := last / avg
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			continue
		}

		if ratio >= anomalyFactor {
			return fmt.Sprintf("%s anomaly: last=%.4f avg=%.4f (%.1fx)", name, last, avg, ratio), scoreAnomaly
		}
		if ratio >= 1.5 {
			return fmt.Sprintf("%s elevated: last=%.4f avg=%.4f (%.1fx)", name, last, avg, ratio), scoreElevated
		}
	}

	return fmt.Sprintf("%s within normal range", name), scoreNormal
}

func firstLabel(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := labels[k]; ok && v != "" {
			return v
		}
	}
	return ""
}
