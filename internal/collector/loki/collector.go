package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	sourceName   = "loki"
	queryLimit   = 500
	scoreFatal   = 0.9
	scoreError   = 0.7
	scoreWarning = 0.4
	scoreDefault = 0.3
)

type Collector struct {
	httpClient *http.Client
	baseURL    string
	log        *zap.Logger
}

func New(rawURL string, log *zap.Logger) (*Collector, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("loki url: %w", err)
	}
	return &Collector{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimRight(u.String(), "/"),
		log:        log,
	}, nil
}

func (c *Collector) Name() string { return sourceName }

func (c *Collector) Collect(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	var (
		mu       sync.Mutex
		evidence []contracts.Evidence
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for _, target := range req.Targets {
		queries := buildLogQueries(target)
		for _, q := range queries {
			g.Go(func() error {
				ev, err := c.executeQuery(gctx, req, target, q)
				if err != nil {
					c.log.Warn("loki query failed",
						zap.String("query", q.expr),
						zap.Error(err),
					)
					return nil
				}
				mu.Lock()
				evidence = append(evidence, ev...)
				mu.Unlock()
				return nil
			})
		}
	}

	if err := g.Wait(); err != nil {
		return evidence, err
	}
	return evidence, nil
}

type logQuery struct {
	name  string
	expr  string
	score float64
}

func buildLogQueries(target contracts.TargetRef) []logQuery {
	ns := target.Namespace
	app := target.Name

	selector := fmt.Sprintf(`{namespace="%s"`, ns)
	if app != "" {
		selector += fmt.Sprintf(`,app="%s"`, app)
	}
	selector += "}"

	return []logQuery{
		{
			name:  "error_logs",
			expr:  selector + ` |= "error" or |= "Error" or |= "ERROR"`,
			score: scoreError,
		},
		{
			name:  "fatal_panic",
			expr:  selector + ` |~ "(?i)(fatal|panic|exception|oom|killed)"`,
			score: scoreFatal,
		},
		{
			name:  "stack_traces",
			expr:  selector + ` |~ "goroutine|stacktrace|Traceback"`,
			score: scoreFatal,
		},
	}
}

type lokiResponse struct {
	Status string   `json:"status"`
	Data   lokiData `json:"data"`
}

type lokiData struct {
	ResultType string       `json:"resultType"`
	Result     []lokiStream `json:"result"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

func (c *Collector) executeQuery(ctx context.Context, req contracts.CollectRequest, target contracts.TargetRef, q logQuery) ([]contracts.Evidence, error) {
	params := url.Values{
		"query": {q.expr},
		"start": {strconv.FormatInt(req.TimeFrom.UnixNano(), 10)},
		"end":   {strconv.FormatInt(req.TimeTo.UnixNano(), 10)},
		"limit": {strconv.Itoa(queryLimit)},
	}

	reqURL := c.baseURL + "/loki/api/v1/query_range?" + params.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("loki request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("loki returned %d: %s", resp.StatusCode, string(body))
	}

	var lokiResp lokiResponse
	if err := json.Unmarshal(body, &lokiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if lokiResp.Status != "success" {
		return nil, fmt.Errorf("loki query status: %s", lokiResp.Status)
	}

	return c.parseStreams(req, target, q, lokiResp.Data.Result), nil
}

func (c *Collector) parseStreams(req contracts.CollectRequest, target contracts.TargetRef, q logQuery, streams []lokiStream) []contracts.Evidence {
	evidence := make([]contracts.Evidence, 0, len(streams))
	now := time.Now().UTC()

	for _, stream := range streams {
		if len(stream.Values) == 0 {
			continue
		}

		lines := extractLines(stream.Values)
		score := scoreForContent(lines, q.score)

		attrs := map[string]string{
			"query":     q.name,
			"log_count": strconv.Itoa(len(stream.Values)),
		}
		for k, v := range stream.Stream {
			attrs["stream."+k] = v
		}

		var earliest, latest time.Time
		if ts, err := parseNanoTimestamp(stream.Values[0][0]); err == nil {
			earliest = ts
		} else {
			earliest = req.TimeFrom
		}
		if ts, err := parseNanoTimestamp(stream.Values[len(stream.Values)-1][0]); err == nil {
			latest = ts
		} else {
			latest = req.TimeTo
		}

		summary := fmt.Sprintf("[%s] %d %s log lines from %s", target.Name, len(stream.Values), q.name, target.Namespace)

		evidence = append(evidence, contracts.Evidence{
			ID:              uuid.NewString(),
			InvestigationID: req.InvestigationID,
			Kind:            contracts.EvidenceLog,
			Source:          sourceName,
			Target:          target,
			CollectedAt:     now,
			ObservedAtFrom:  earliest,
			ObservedAtTo:    latest,
			Summary:         summary,
			BodyRef:         truncateBody(lines, 8192),
			Query:           q.expr,
			Score:           score,
			Attributes:      attrs,
			RedactionState:  contracts.RedactionNone,
		})
	}

	return evidence
}

func extractLines(values [][]string) string {
	var b strings.Builder
	for _, pair := range values {
		if len(pair) < 2 {
			continue
		}
		b.WriteString(pair[1])
		b.WriteByte('\n')
	}
	return b.String()
}

func scoreForContent(content string, base float64) float64 {
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") || strings.Contains(lower, "oomkilled"):
		return scoreFatal
	case strings.Contains(lower, "error") || strings.Contains(lower, "exception"):
		if base > scoreError {
			return base
		}
		return scoreError
	case strings.Contains(lower, "warning") || strings.Contains(lower, "warn"):
		if base > scoreWarning {
			return base
		}
		return scoreWarning
	default:
		return base
	}
}

func parseNanoTimestamp(s string) (time.Time, error) {
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ns).UTC(), nil
}

func truncateBody(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... truncated ..."
}
