package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/slack-go/slack"
	"go.uber.org/zap"
)

type slackAPI interface {
	PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error)
}

type Publisher struct {
	slackClient slackAPI
	logger      *zap.Logger
}

func NewPublisher(botToken string, logger *zap.Logger) *Publisher {
	return &Publisher{
		slackClient: slack.New(botToken),
		logger:      logger,
	}
}

func (p *Publisher) PostInvestigationStarted(ctx context.Context, channelID string, investigationID string) (string, error) {
	text := "Investigating... Sherlock is on the case."
	if investigationID != "" {
		text = fmt.Sprintf("Investigating [%s]... Sherlock is on the case.", investigationID)
	}

	_, ts, err := p.slackClient.PostMessageContext(ctx, channelID,
		slack.MsgOptionText(text, false),
	)
	if err != nil {
		return "", fmt.Errorf("post investigation started: %w", err)
	}
	return ts, nil
}

func (p *Publisher) PostEvidenceUpdate(ctx context.Context, channelID, threadTS string, message string) error {
	_, _, err := p.slackClient.PostMessageContext(ctx, channelID,
		slack.MsgOptionText(message, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return fmt.Errorf("post evidence update: %w", err)
	}
	return nil
}

func (p *Publisher) PostResult(ctx context.Context, channelID, threadTS string, result *contracts.InvestigationResult) error {
	blocks := p.buildResultBlocks(result)

	_, _, err := p.slackClient.PostMessageContext(ctx, channelID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return fmt.Errorf("post result: %w", err)
	}
	return nil
}

func (p *Publisher) NotifyPassStarted(ctx context.Context, channelID, threadTS string, _ int, message string) error {
	_, _, err := p.slackClient.PostMessageContext(ctx, channelID,
		slack.MsgOptionText(message, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return fmt.Errorf("post pass update: %w", err)
	}
	return nil
}

func (p *Publisher) PostError(ctx context.Context, channelID, threadTS string, errMsg string) error {
	text := fmt.Sprintf("Investigation failed: %s", errMsg)

	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	_, _, err := p.slackClient.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("post error: %w", err)
	}
	return nil
}

func (p *Publisher) PostDedupNotification(ctx context.Context, channelID, threadTS, existingID, alertValue string) error {
	headerText := fmt.Sprintf("Duplicate alert detected — linked to existing investigation `%s`. Skipping re-investigation.", existingID)

	headerSection := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, headerText, false, false),
		nil, nil,
	)

	forceBtn := slack.NewButtonBlockElement(
		"investigate_force_reinvestigate",
		alertValue,
		slack.NewTextBlockObject(slack.PlainTextType, "Force Re-investigate", false, false),
	)
	forceBtn.Style = slack.StyleDanger

	actionsBlock := slack.NewActionBlock("dedup_actions", forceBtn)

	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(headerSection, actionsBlock),
	}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	_, _, err := p.slackClient.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("post dedup notification: %w", err)
	}
	return nil
}

func (p *Publisher) buildResultBlocks(result *contracts.InvestigationResult) []slack.Block {
	if result.RCAEngine == "llm-powered" {
		return p.buildLLMResultBlocks(result)
	}
	return p.buildRuleResultBlocks(result)
}

func (p *Publisher) buildRuleResultBlocks(result *contracts.InvestigationResult) []slack.Block {
	var blocks []slack.Block

	headerText := slack.NewTextBlockObject(slack.PlainTextType, result.Headline, false, false)
	blocks = append(blocks, slack.NewHeaderBlock(headerText))

	confText := fmt.Sprintf("*Confidence:* %s (%.0f%%)", confidenceIndicator(result.Confidence), result.Confidence*100)
	confSection := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, confText, false, false),
		nil, nil,
	)
	blocks = append(blocks, confSection)

	if len(result.TopHypotheses) > 0 {
		top := result.TopHypotheses[0]
		hypoText := fmt.Sprintf("*Top Hypothesis:* %s\n%s", top.Title, top.Narrative)
		hypoSection := slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, hypoText, false, false),
			nil, nil,
		)
		blocks = append(blocks, hypoSection)
	}

	if len(result.RecommendedActions) > 0 {
		var actionLines []string
		for _, a := range result.RecommendedActions {
			actionLines = append(actionLines, fmt.Sprintf("- %s", a.Title))
		}
		actionsText := fmt.Sprintf("*Recommended Actions:*\n%s", strings.Join(actionLines, "\n"))
		actionsSection := slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, actionsText, false, false),
			nil, nil,
		)
		blocks = append(blocks, actionsSection)
	}

	evidenceSummary := fmt.Sprintf("*Evidence:* %d timeline events referenced", len(result.TimelineEventIDs))
	evidenceSection := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, evidenceSummary, false, false),
		nil, nil,
	)
	blocks = append(blocks, evidenceSection)

	blocks = append(blocks, slack.NewDividerBlock())
	blocks = append(blocks, p.buildActionButtons(result)...)

	return blocks
}

func (p *Publisher) buildLLMResultBlocks(result *contracts.InvestigationResult) []slack.Block {
	var blocks []slack.Block

	badge := fmt.Sprintf("Deep Investigation (%d-Pass Analysis)", result.PassCount)
	if result.PassCount <= 1 {
		badge = "AI-Powered Investigation"
	}
	headerText := slack.NewTextBlockObject(slack.PlainTextType, badge, false, false)
	blocks = append(blocks, slack.NewHeaderBlock(headerText))

	confColor := confidenceColor(result.Confidence)
	confLabel := confidenceIndicator(result.Confidence)
	summaryText := fmt.Sprintf("%s *Confidence: %s (%.0f%%)*\n\n*Summary:* %s",
		confColor, confLabel, result.Confidence*100, result.Headline)
	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, summaryText, false, false),
		nil, nil,
	))

	if result.RootCause != "" {
		rootCauseText := fmt.Sprintf("*Root Cause:*\n%s", truncateSlackText(result.RootCause, 2000))
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, rootCauseText, false, false),
			nil, nil,
		))
	}

	if result.Severity != "" {
		severityText := fmt.Sprintf("*Severity:* %s", strings.ToUpper(result.Severity))
		if result.BugFixable {
			severityText += "  |  *Bug Fixable:* Yes"
		}
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, severityText, false, false),
			nil, nil,
		))
	}

	blocks = append(blocks, slack.NewDividerBlock())

	if len(result.RecommendedActions) > 0 {
		var fixLines []string
		for i, a := range result.RecommendedActions {
			if i >= 5 {
				fixLines = append(fixLines, fmt.Sprintf("_... and %d more_", len(result.RecommendedActions)-5))
				break
			}
			line := fmt.Sprintf("*%d.* %s", i+1, a.Title)
			if a.Description != "" {
				line += fmt.Sprintf("\n    %s", truncateSlackText(a.Description, 200))
			}
			fixLines = append(fixLines, line)
		}
		fixText := fmt.Sprintf("*Recommendations:*\n%s", strings.Join(fixLines, "\n"))
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, fixText, false, false),
			nil, nil,
		))
	}

	evidenceSummary := fmt.Sprintf("*Evidence:* %d timeline events referenced", len(result.TimelineEventIDs))
	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, evidenceSummary, false, false),
		nil, nil,
	))

	blocks = append(blocks, slack.NewDividerBlock())

	var footerParts []string
	if result.AIProvider != "" {
		footerParts = append(footerParts, fmt.Sprintf("Provider: %s", result.AIProvider))
	}
	if result.AIModel != "" {
		footerParts = append(footerParts, fmt.Sprintf("Model: %s", result.AIModel))
	}
	if result.PassCount > 0 {
		footerParts = append(footerParts, fmt.Sprintf("Passes: %d", result.PassCount))
	}
	if len(footerParts) > 0 {
		footerText := fmt.Sprintf("_%s_", strings.Join(footerParts, " | "))
		blocks = append(blocks, slack.NewContextBlock("investigation_footer",
			slack.NewTextBlockObject(slack.MarkdownType, footerText, false, false),
		))
	}

	blocks = append(blocks, p.buildActionButtons(result)...)

	return blocks
}

func (p *Publisher) buildActionButtons(result *contracts.InvestigationResult) []slack.Block {
	rerunBtn := slack.NewButtonBlockElement("investigate_rerun", result.InvestigationID,
		slack.NewTextBlockObject(slack.PlainTextType, "Re-run Investigation", false, false),
	)
	expandBtn := slack.NewButtonBlockElement("investigate_expand_evidence", result.InvestigationID,
		slack.NewTextBlockObject(slack.PlainTextType, "View Evidence", false, false),
	)
	runbookBtn := slack.NewButtonBlockElement("investigate_open_runbook", result.InvestigationID,
		slack.NewTextBlockObject(slack.PlainTextType, "Open Runbook", false, false),
	)
	suppressBtn := slack.NewButtonBlockElement("investigate_suppress_similar", result.InvestigationID,
		slack.NewTextBlockObject(slack.PlainTextType, "Suppress Similar", false, false),
	)
	suppressBtn.Style = slack.StyleDanger
	compareBtn := slack.NewButtonBlockElement("investigate_compare_deployments", result.InvestigationID,
		slack.NewTextBlockObject(slack.PlainTextType, "Compare Deployments", false, false),
	)
	issueBtn := slack.NewButtonBlockElement("investigate_create_issue", result.InvestigationID,
		slack.NewTextBlockObject(slack.PlainTextType, "Create Issue", false, false),
	)

	actionsBlock := slack.NewActionBlock("investigation_actions", rerunBtn, expandBtn, runbookBtn, suppressBtn, compareBtn)
	issueActionsBlock := slack.NewActionBlock("investigation_actions_2", issueBtn)
	return []slack.Block{actionsBlock, issueActionsBlock}
}

func confidenceColor(conf float64) string {
	switch {
	case conf > 0.7:
		return ":large_green_circle:"
	case conf >= 0.4:
		return ":large_yellow_circle:"
	default:
		return ":red_circle:"
	}
}

func truncateSlackText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func confidenceIndicator(conf float64) string {
	switch {
	case conf > 0.7:
		return "High"
	case conf >= 0.4:
		return "Medium"
	default:
		return "Low"
	}
}
