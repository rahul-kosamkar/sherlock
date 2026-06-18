package rca

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/llm"
	"go.uber.org/zap"
)

var _ contracts.RCAEngine = (*LLMEngine)(nil)

type LLMEngineConfig struct {
	MaxPasses int
}

type PassNotifier interface {
	NotifyPassStarted(ctx context.Context, channelID, threadTS string, passNum int, message string) error
}

type LLMEngine struct {
	provider  llm.Provider
	followUp  *llm.FollowUpExecutor
	fallback  contracts.RCAEngine
	maxPasses int
	logger    *zap.Logger
	notifier  PassNotifier
}

func NewLLMEngine(
	provider llm.Provider,
	followUp *llm.FollowUpExecutor,
	fallback contracts.RCAEngine,
	cfg LLMEngineConfig,
	logger *zap.Logger,
) *LLMEngine {
	maxPasses := cfg.MaxPasses
	if maxPasses <= 0 {
		maxPasses = 3
	}

	return &LLMEngine{
		provider:  provider,
		followUp:  followUp,
		fallback:  fallback,
		maxPasses: maxPasses,
		logger:    logger,
	}
}

func (e *LLMEngine) SetNotifier(n PassNotifier) {
	e.notifier = n
}

func (e *LLMEngine) Name() string {
	return "llm-powered"
}

func (e *LLMEngine) Rank(ctx context.Context, graph contracts.InvestigationGraph) ([]contracts.Hypothesis, error) {
	hypotheses, err := e.rankWithLLM(ctx, graph)
	if err != nil {
		e.logger.Warn("LLM analysis failed, falling back to rule-based engine",
			zap.Error(err),
			zap.String("llm_provider", e.provider.Name()),
		)
		return e.fallback.Rank(ctx, graph)
	}
	return hypotheses, nil
}

func (e *LLMEngine) rankWithLLM(ctx context.Context, graph contracts.InvestigationGraph) ([]contracts.Hypothesis, error) {
	data := graph.Data
	alert := data.Alerts[0]

	bundle := llm.BuildEvidenceBundleFromCollected(alert, data.Evidence)

	channelID := data.Investigation.SlackChannelID
	threadTS := data.Investigation.SlackThreadTS

	e.notifyPass(ctx, channelID, threadTS, 1,
		fmt.Sprintf("Pass 1/%d: Analyzing %d evidence items with %s...", e.maxPasses, len(data.Evidence), e.provider.Name()))

	pass1Prompt := llm.BuildPass1Prompt(bundle)

	pass1Resp, err := e.provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: "You are Sherlock, an expert SRE AI that investigates production incidents by analyzing evidence systematically.",
		UserPrompt:   pass1Prompt,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM pass 1 failed: %w", err)
	}

	analysis := llm.ParseAnalysis(pass1Resp.Content)
	analysis.PassCount = 1
	analysis.AIProvider = e.provider.Name()

	e.logger.Info("LLM pass 1 complete",
		zap.String("summary", analysis.Summary),
		zap.String("confidence", analysis.Confidence),
		zap.Int("follow_ups", len(analysis.FollowUps)),
		zap.Int("input_tokens", pass1Resp.InputTokens),
		zap.Int("output_tokens", pass1Resp.OutputTokens),
		zap.Duration("latency", pass1Resp.Latency),
	)

	for pass := 2; pass <= e.maxPasses; pass++ {
		if len(analysis.FollowUps) == 0 {
			e.logger.Info("no follow-ups requested, ending multi-pass loop", zap.Int("pass", pass-1))
			break
		}

		if pass > 2 && analysis.Confidence == "high" {
			e.logger.Info("confidence is high, skipping further passes", zap.Int("pass", pass))
			break
		}

		e.notifyPass(ctx, channelID, threadTS, pass,
			fmt.Sprintf("Pass %d/%d: Gathering %d follow-up data requests...", pass, e.maxPasses, len(analysis.FollowUps)))

		targets := data.Investigation.Targets
		deepEvidence, err := e.followUp.Execute(ctx, analysis.FollowUps, alert, targets)
		if err != nil {
			e.logger.Warn("follow-up execution failed", zap.Int("pass", pass), zap.Error(err))
			break
		}

		if isDeepEvidenceEmpty(deepEvidence) {
			e.logger.Info("no additional evidence gathered, ending multi-pass loop", zap.Int("pass", pass))
			break
		}

		e.notifyPass(ctx, channelID, threadTS, pass,
			fmt.Sprintf("Pass %d/%d: Deep analysis with additional evidence...", pass, e.maxPasses))

		deepPrompt := llm.BuildDeepPassPrompt(bundle, analysis, deepEvidence, pass)

		deepResp, err := e.provider.Complete(ctx, llm.CompletionRequest{
			SystemPrompt: "You are Sherlock, an expert SRE AI performing deep analysis of a production incident with additional evidence.",
			UserPrompt:   deepPrompt,
		})
		if err != nil {
			e.logger.Warn("LLM deep pass failed", zap.Int("pass", pass), zap.Error(err))
			break
		}

		analysis = llm.ParseAnalysis(deepResp.Content)
		analysis.PassCount = pass
		analysis.DeepDive = true
		analysis.AIProvider = e.provider.Name()

		e.logger.Info("LLM deep pass complete",
			zap.Int("pass", pass),
			zap.String("summary", analysis.Summary),
			zap.String("confidence", analysis.Confidence),
			zap.Int("follow_ups", len(analysis.FollowUps)),
			zap.Int("input_tokens", deepResp.InputTokens),
			zap.Int("output_tokens", deepResp.OutputTokens),
			zap.Duration("latency", deepResp.Latency),
		)
	}

	hypotheses := llm.MapToHypotheses(analysis, data.Evidence)
	for i := range hypotheses {
		hypotheses[i].ID = uuid.New().String()
	}

	if len(hypotheses) == 0 || hypotheses[0].Title == "" {
		e.logger.Warn("LLM produced no meaningful hypotheses, falling back to rules")
		return e.fallback.Rank(ctx, graph)
	}

	return hypotheses, nil
}

func (e *LLMEngine) notifyPass(ctx context.Context, channelID, threadTS string, pass int, message string) {
	if e.notifier == nil || channelID == "" {
		return
	}
	if err := e.notifier.NotifyPassStarted(ctx, channelID, threadTS, pass, message); err != nil {
		e.logger.Warn("failed to notify pass update", zap.Int("pass", pass), zap.Error(err))
	}
}

func isDeepEvidenceEmpty(deep *llm.DeepEvidence) bool {
	if deep == nil {
		return true
	}
	return len(deep.TraceLogs) == 0 &&
		strings.TrimSpace(deep.ExtraLogs) == "" &&
		strings.TrimSpace(deep.ExtraPodEvents) == "" &&
		len(deep.ExtraSourceFiles) == 0 &&
		strings.TrimSpace(deep.CustomQueryResults) == ""
}
