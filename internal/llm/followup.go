package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/git"
	"go.uber.org/zap"
)

type CollectorSet interface {
	CollectAll(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error)
}

type FollowUpExecutor struct {
	collectors CollectorSet
	gitProv    git.Provider
	logger     *zap.Logger
}

func NewFollowUpExecutor(collectors CollectorSet, gitProv git.Provider, logger *zap.Logger) *FollowUpExecutor {
	return &FollowUpExecutor{
		collectors: collectors,
		gitProv:    gitProv,
		logger:     logger,
	}
}

func (f *FollowUpExecutor) Execute(ctx context.Context, followUps []FollowUpQuery, alert contracts.NormalizedAlert, targets []contracts.TargetRef) (*DeepEvidence, error) {
	deep := &DeepEvidence{
		TraceLogs:        make(map[string]string),
		ExtraSourceFiles: make(map[string]string),
	}

	for _, fu := range followUps {
		switch strings.ToUpper(fu.Tool) {
		case "TRACE_LOGS":
			f.executeTraceLogs(ctx, fu, alert, targets, deep)
		case "TIME_WINDOW_LOGS":
			f.executeTimeWindowLogs(ctx, fu, alert, targets, deep)
		case "POD_EVENTS":
			f.executePodEvents(ctx, alert, targets, deep)
		case "GITHUB_FILES":
			f.executeGitHubFiles(ctx, fu, alert, deep)
		case "LOG_QUERY":
			f.executeLogQuery(ctx, fu, alert, targets, deep)
		default:
			f.logger.Warn("unknown follow-up tool requested", zap.String("tool", fu.Tool))
		}
	}

	return deep, nil
}

func (f *FollowUpExecutor) executeTraceLogs(ctx context.Context, fu FollowUpQuery, alert contracts.NormalizedAlert, targets []contracts.TargetRef, deep *DeepEvidence) {
	traceIDs := parseCSV(fu.Value)
	for _, traceID := range traceIDs {
		traceID = strings.TrimSpace(traceID)
		if traceID == "" {
			continue
		}

		req := contracts.CollectRequest{
			InvestigationID: "followup",
			Alert:           alert,
			Targets:         targets,
			TimeFrom:        alert.StartsAt.Add(-30 * time.Minute),
			TimeTo:          time.Now().UTC(),
		}
		req.Alert.Labels["trace_id"] = traceID

		evidence, err := f.collectors.CollectAll(ctx, req)
		if err != nil {
			f.logger.Warn("trace log collection failed", zap.String("trace_id", traceID), zap.Error(err))
			continue
		}

		var logContent strings.Builder
		for _, e := range evidence {
			if e.Kind == contracts.EvidenceLog {
				logContent.WriteString(e.Summary)
				logContent.WriteString("\n")
			}
		}
		if logContent.Len() > 0 {
			deep.TraceLogs[traceID] = logContent.String()
		}
	}
}

func (f *FollowUpExecutor) executeTimeWindowLogs(ctx context.Context, fu FollowUpQuery, alert contracts.NormalizedAlert, targets []contracts.TargetRef, deep *DeepEvidence) {
	timeFrom, timeTo := parseTimeWindow(fu.Value)

	req := contracts.CollectRequest{
		InvestigationID: "followup",
		Alert:           alert,
		Targets:         targets,
		TimeFrom:        timeFrom,
		TimeTo:          timeTo,
	}

	evidence, err := f.collectors.CollectAll(ctx, req)
	if err != nil {
		f.logger.Warn("time window log collection failed", zap.Error(err))
		return
	}

	var logContent strings.Builder
	for _, e := range evidence {
		if e.Kind == contracts.EvidenceLog {
			logContent.WriteString(e.Summary)
			logContent.WriteString("\n")
		}
	}
	deep.ExtraLogs = logContent.String()
}

func (f *FollowUpExecutor) executePodEvents(ctx context.Context, alert contracts.NormalizedAlert, targets []contracts.TargetRef, deep *DeepEvidence) {
	req := contracts.CollectRequest{
		InvestigationID: "followup",
		Alert:           alert,
		Targets:         targets,
		TimeFrom:        time.Now().UTC().Add(-1 * time.Hour),
		TimeTo:          time.Now().UTC(),
	}

	evidence, err := f.collectors.CollectAll(ctx, req)
	if err != nil {
		f.logger.Warn("pod events collection failed", zap.Error(err))
		return
	}

	var eventContent strings.Builder
	for _, e := range evidence {
		if e.Kind == contracts.EvidenceEvent || e.Kind == contracts.EvidenceK8sState {
			eventContent.WriteString(e.Summary)
			eventContent.WriteString("\n")
		}
	}
	deep.ExtraPodEvents = eventContent.String()
}

func (f *FollowUpExecutor) executeGitHubFiles(ctx context.Context, fu FollowUpQuery, alert contracts.NormalizedAlert, deep *DeepEvidence) {
	if f.gitProv == nil {
		f.logger.Debug("git provider not configured, skipping GITHUB_FILES follow-up")
		return
	}

	paths := parseCSV(fu.Value)

	safePaths := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || strings.Contains(p, "..") || strings.HasPrefix(p, "/") {
			f.logger.Warn("rejecting unsafe file path", zap.String("path", p))
			continue
		}
		safePaths = append(safePaths, p)
	}
	paths = safePaths

	workload := resolveWorkload(alert)

	repo, ok := f.gitProv.ResolveRepo(workload)
	if !ok {
		f.logger.Warn("no repo mapping for workload", zap.String("workload", workload))
		return
	}

	files, err := f.gitProv.FetchFiles(ctx, repo, paths)
	if err != nil {
		f.logger.Warn("git file fetch failed", zap.Error(err))
		return
	}

	for path, content := range files {
		deep.ExtraSourceFiles[path] = content
	}
}

func (f *FollowUpExecutor) executeLogQuery(ctx context.Context, fu FollowUpQuery, alert contracts.NormalizedAlert, targets []contracts.TargetRef, deep *DeepEvidence) {
	req := contracts.CollectRequest{
		InvestigationID: "followup",
		Alert:           alert,
		Targets:         targets,
		TimeFrom:        alert.StartsAt.Add(-30 * time.Minute),
		TimeTo:          time.Now().UTC(),
	}
	req.Alert.Labels["custom_query"] = fu.Value

	evidence, err := f.collectors.CollectAll(ctx, req)
	if err != nil {
		f.logger.Warn("custom log query failed", zap.Error(err))
		return
	}

	var queryResult strings.Builder
	for _, e := range evidence {
		if e.Kind == contracts.EvidenceLog {
			queryResult.WriteString(e.Summary)
			queryResult.WriteString("\n")
		}
	}
	if queryResult.Len() > 0 {
		deep.CustomQueryResults = queryResult.String()
	}
}

func parseCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func parseTimeWindow(value string) (time.Time, time.Time) {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 {
		now := time.Now().UTC()
		return now.Add(-30 * time.Minute), now
	}

	timeFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[0]))
	if err != nil {
		now := time.Now().UTC()
		return now.Add(-30 * time.Minute), now
	}

	timeTo, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
	if err != nil {
		return timeFrom, time.Now().UTC()
	}

	return timeFrom, timeTo
}

func resolveWorkload(alert contracts.NormalizedAlert) string {
	for _, key := range []string{"service", "app", "workload", "deployment"} {
		if v, ok := alert.Labels[key]; ok && v != "" {
			return v
		}
	}
	return fmt.Sprintf("unknown-%s", alert.ID)
}
