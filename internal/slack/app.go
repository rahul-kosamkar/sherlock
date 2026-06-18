package slack

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/slack/transport"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type InvestigationEnqueuer interface {
	EnqueueInvestigation(ctx context.Context, channelID, threadTS, userID, text string) (string, error)
}

type EvidenceQuerier interface {
	ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.Evidence, error)
}

type AlertQuerier interface {
	GetByID(ctx context.Context, id string) (*contracts.NormalizedAlert, error)
}

type InvestigationQuerier interface {
	GetByID(ctx context.Context, id string) (*contracts.Investigation, error)
}

type SuppressionStore interface {
	Create(ctx context.Context, fingerprint string, expiresAt time.Time, createdBy, reason string) error
	IsActive(ctx context.Context, fingerprint string) (bool, error)
}

type AppConfig struct {
	BotToken      string
	AppToken      string
	SigningSecret string
	Mode          string // "socket", "http", "both"
	HTTPAddress   string
}

type App struct {
	transports     []transport.Transport
	enqueuer       InvestigationEnqueuer
	publisher      *Publisher
	evidence       EvidenceQuerier
	alerts         AlertQuerier
	investigations InvestigationQuerier
	suppressions   SuppressionStore
	logger         *zap.Logger
}

func NewApp(cfg AppConfig, enqueuer InvestigationEnqueuer, publisher *Publisher, evidence EvidenceQuerier, alerts AlertQuerier, investigations InvestigationQuerier, suppressions SuppressionStore, logger *zap.Logger) (*App, error) {
	a := &App{
		enqueuer:       enqueuer,
		publisher:      publisher,
		evidence:       evidence,
		alerts:         alerts,
		investigations: investigations,
		suppressions:   suppressions,
		logger:         logger,
	}

	switch cfg.Mode {
	case "socket":
		if cfg.AppToken == "" || cfg.BotToken == "" {
			return nil, fmt.Errorf("socket mode requires app_token and bot_token")
		}
		st := transport.NewSocketTransport(cfg.AppToken, cfg.BotToken, a, logger)
		a.transports = append(a.transports, st)

	case "http":
		if cfg.SigningSecret == "" {
			return nil, fmt.Errorf("http mode requires signing_secret")
		}
		addr := cfg.HTTPAddress
		if addr == "" {
			addr = ":3000"
		}
		ht := transport.NewHTTPTransport(addr, cfg.SigningSecret, a, logger)
		a.transports = append(a.transports, ht)

	case "both":
		if cfg.AppToken == "" || cfg.BotToken == "" {
			return nil, fmt.Errorf("both mode requires app_token and bot_token")
		}
		if cfg.SigningSecret == "" {
			return nil, fmt.Errorf("both mode requires signing_secret")
		}
		addr := cfg.HTTPAddress
		if addr == "" {
			addr = ":3001"
		}
		st := transport.NewSocketTransport(cfg.AppToken, cfg.BotToken, a, logger)
		ht := transport.NewHTTPTransport(addr, cfg.SigningSecret, a, logger)
		a.transports = append(a.transports, st, ht)

	default:
		return nil, fmt.Errorf("unknown transport mode: %s", cfg.Mode)
	}

	return a, nil
}

func (a *App) Start(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, t := range a.transports {
		t := t
		g.Go(func() error {
			return t.Start(ctx)
		})
	}
	return g.Wait()
}

func (a *App) Stop() error {
	var errs []string
	for _, t := range a.transports {
		if err := t.Stop(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("transport stop errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (a *App) HandleSlashCommand(ctx context.Context, cmd transport.SlashCommand) error {
	a.logger.Info("received slash command",
		zap.String("command", cmd.Command),
		zap.String("text", cmd.Text),
		zap.String("user_id", cmd.UserID),
		zap.String("channel_id", cmd.ChannelID),
	)

	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		a.logger.Warn("empty slash command text, ignoring")
		return nil
	}

	threadTS, err := a.publisher.PostInvestigationStarted(ctx, cmd.ChannelID, "")
	if err != nil {
		a.logger.Error("failed to post investigation started", zap.Error(err))
		return fmt.Errorf("post investigation started: %w", err)
	}

	investigationID, err := a.enqueuer.EnqueueInvestigation(ctx, cmd.ChannelID, threadTS, cmd.UserID, text)
	if err != nil {
		a.logger.Error("failed to enqueue investigation", zap.Error(err))
		_ = a.publisher.PostError(ctx, cmd.ChannelID, threadTS, "Failed to start investigation: "+err.Error())
		return fmt.Errorf("enqueue investigation: %w", err)
	}

	a.logger.Info("investigation enqueued",
		zap.String("investigation_id", investigationID),
		zap.String("channel_id", cmd.ChannelID),
		zap.String("thread_ts", threadTS),
	)
	return nil
}

func (a *App) HandleMessageShortcut(ctx context.Context, shortcut transport.MessageShortcut) error {
	a.logger.Info("received message shortcut",
		zap.String("callback_id", shortcut.CallbackID),
		zap.String("user_id", shortcut.UserID),
		zap.String("channel_id", shortcut.ChannelID),
		zap.String("message_ts", shortcut.MessageTS),
	)

	text := strings.TrimSpace(shortcut.MessageText)
	if text == "" {
		text = "investigate message " + shortcut.MessageTS
	}

	threadTS, err := a.publisher.PostInvestigationStarted(ctx, shortcut.ChannelID, "")
	if err != nil {
		a.logger.Error("failed to post investigation started", zap.Error(err))
		return fmt.Errorf("post investigation started: %w", err)
	}

	investigationID, err := a.enqueuer.EnqueueInvestigation(ctx, shortcut.ChannelID, threadTS, shortcut.UserID, text)
	if err != nil {
		a.logger.Error("failed to enqueue investigation", zap.Error(err))
		_ = a.publisher.PostError(ctx, shortcut.ChannelID, threadTS, "Failed to start investigation: "+err.Error())
		return fmt.Errorf("enqueue investigation: %w", err)
	}

	a.logger.Info("investigation enqueued from shortcut",
		zap.String("investigation_id", investigationID),
		zap.String("channel_id", shortcut.ChannelID),
		zap.String("thread_ts", threadTS),
	)
	return nil
}

func (a *App) HandleInteraction(ctx context.Context, action transport.InteractionAction) error {
	a.logger.Info("received interaction",
		zap.String("action_id", action.ActionID),
		zap.String("value", action.Value),
		zap.String("user_id", action.UserID),
		zap.String("channel_id", action.ChannelID),
	)

	switch action.ActionID {
	case "investigate_rerun":
		threadTS, err := a.publisher.PostInvestigationStarted(ctx, action.ChannelID, "")
		if err != nil {
			return fmt.Errorf("post investigation started: %w", err)
		}
		_, err = a.enqueuer.EnqueueInvestigation(ctx, action.ChannelID, threadTS, action.UserID, action.Value)
		if err != nil {
			_ = a.publisher.PostError(ctx, action.ChannelID, threadTS, "Failed to re-run investigation: "+err.Error())
			return fmt.Errorf("enqueue re-run investigation: %w", err)
		}
		return nil

	case "investigate_force_reinvestigate":
		a.logger.Info("force re-investigate requested, bypassing dedup",
			zap.String("value", action.Value),
			zap.String("user_id", action.UserID),
		)
		threadTS, err := a.publisher.PostInvestigationStarted(ctx, action.ChannelID, "")
		if err != nil {
			return fmt.Errorf("post investigation started: %w", err)
		}
		_, err = a.enqueuer.EnqueueInvestigation(ctx, action.ChannelID, threadTS, action.UserID, action.Value)
		if err != nil {
			_ = a.publisher.PostError(ctx, action.ChannelID, threadTS, "Failed to force re-investigate: "+err.Error())
			return fmt.Errorf("enqueue force re-investigate: %w", err)
		}
		return nil

	case "investigate_expand_evidence":
		return a.handleExpandEvidence(ctx, action)

	case "investigate_open_runbook":
		return a.handleOpenRunbook(ctx, action)

	case "investigate_suppress_similar":
		return a.handleSuppressSimilar(ctx, action)

	case "investigate_compare_deployments":
		return a.handleCompareDeployments(ctx, action)

	case "investigate_create_issue":
		return a.handleCreateIssue(ctx, action)

	default:
		a.logger.Warn("unknown action", zap.String("action_id", action.ActionID))
		return nil
	}
}

func (a *App) handleExpandEvidence(ctx context.Context, action transport.InteractionAction) error {
	investigationID := action.Value
	threadTS := action.MessageTS

	evidence, err := a.evidence.ListByInvestigation(ctx, investigationID)
	if err != nil {
		a.logger.Error("failed to fetch evidence", zap.Error(err), zap.String("investigation_id", investigationID))
		_ = a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "Failed to retrieve evidence.")
		return fmt.Errorf("list evidence: %w", err)
	}

	if len(evidence) == 0 {
		return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "No evidence collected for this investigation.")
	}

	grouped := make(map[contracts.EvidenceKind][]contracts.Evidence)
	for _, e := range evidence {
		grouped[e.Kind] = append(grouped[e.Kind], e)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*Evidence for investigation %s:*\n", investigationID))

	kindOrder := []contracts.EvidenceKind{
		contracts.EvidenceK8sState,
		contracts.EvidenceLog,
		contracts.EvidenceMetric,
		contracts.EvidenceEvent,
		contracts.EvidenceDeploy,
		contracts.EvidenceGitChange,
		contracts.EvidenceTrace,
		contracts.EvidenceConfig,
	}

	for _, kind := range kindOrder {
		items, ok := grouped[kind]
		if !ok {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n*[%s]* (%d items)\n", string(kind), len(items)))
		for _, item := range items {
			line := fmt.Sprintf("• %s\n", item.Summary)
			if sb.Len()+len(line) > 2800 {
				sb.WriteString("• _... truncated_\n")
				break
			}
			sb.WriteString(line)
		}
		if sb.Len() > 2800 {
			break
		}
	}

	return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, sb.String())
}

var botMentionRE = regexp.MustCompile(`<@[A-Z0-9]+>\s*`)

func (a *App) HandleAppMention(ctx context.Context, channelID, userID, text string) error {
	a.logger.Info("received app mention",
		zap.String("channel_id", channelID),
		zap.String("user_id", userID),
		zap.String("text", text),
	)

	cleaned := strings.TrimSpace(botMentionRE.ReplaceAllString(text, ""))
	lower := strings.ToLower(cleaned)

	switch {
	case strings.Contains(lower, "help"):
		return a.publisher.PostEvidenceUpdate(ctx, channelID, "",
			"*Sherlock Commands:*\n"+
				"• `@sherlock investigate <service>` — start an investigation\n"+
				"• `@sherlock status <investigation-id>` — check investigation status\n"+
				"• `@sherlock help` — show this message")

	case strings.Contains(lower, "investigate"):
		service := extractArg(cleaned, "investigate")
		if service == "" {
			return a.publisher.PostEvidenceUpdate(ctx, channelID, "",
				"Please specify a service to investigate. Example: `@sherlock investigate payment-api`")
		}
		threadTS, err := a.publisher.PostInvestigationStarted(ctx, channelID, "")
		if err != nil {
			return fmt.Errorf("post investigation started: %w", err)
		}
		_, err = a.enqueuer.EnqueueInvestigation(ctx, channelID, threadTS, userID, service)
		if err != nil {
			_ = a.publisher.PostError(ctx, channelID, threadTS, "Failed to start investigation: "+err.Error())
			return fmt.Errorf("enqueue investigation: %w", err)
		}
		return nil

	case strings.Contains(lower, "status"):
		invID := extractArg(cleaned, "status")
		if invID == "" {
			return a.publisher.PostEvidenceUpdate(ctx, channelID, "",
				"Please specify an investigation ID. Example: `@sherlock status inv-abc123`")
		}
		inv, err := a.investigations.GetByID(ctx, invID)
		if err != nil {
			return a.publisher.PostEvidenceUpdate(ctx, channelID, "",
				fmt.Sprintf("Could not find investigation `%s`.", invID))
		}
		msg := fmt.Sprintf("*Investigation %s*\n• Status: `%s`\n• Headline: %s\n• Confidence: %.0f%%",
			inv.ID, inv.Status, inv.Headline, inv.Confidence*100)
		return a.publisher.PostEvidenceUpdate(ctx, channelID, "", msg)

	default:
		return a.publisher.PostEvidenceUpdate(ctx, channelID, "",
			"I didn't understand that. Try `@sherlock help` for available commands.")
	}
}

func extractArg(text, keyword string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, keyword)
	if idx < 0 {
		return ""
	}
	after := strings.TrimSpace(text[idx+len(keyword):])
	parts := strings.Fields(after)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (a *App) handleOpenRunbook(ctx context.Context, action transport.InteractionAction) error {
	investigationID := action.Value
	threadTS := action.MessageTS

	inv, err := a.investigations.GetByID(ctx, investigationID)
	if err != nil {
		a.logger.Error("failed to fetch investigation", zap.Error(err), zap.String("investigation_id", investigationID))
		_ = a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "Failed to look up investigation for runbook.")
		return fmt.Errorf("get investigation: %w", err)
	}

	for _, alertID := range inv.AlertIDs {
		alert, alertErr := a.alerts.GetByID(ctx, alertID)
		if alertErr != nil {
			a.logger.Warn("failed to fetch alert", zap.Error(alertErr), zap.String("alert_id", alertID))
			continue
		}
		if url, ok := alert.Annotations["runbook_url"]; ok && url != "" {
			msg := fmt.Sprintf("*Runbook:* <%s|Open Runbook>", url)
			return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, msg)
		}
	}

	return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "No runbook URL configured for this alert.")
}

func (a *App) handleSuppressSimilar(ctx context.Context, action transport.InteractionAction) error {
	investigationID := action.Value
	threadTS := action.MessageTS

	inv, err := a.investigations.GetByID(ctx, investigationID)
	if err != nil {
		a.logger.Error("failed to fetch investigation for suppression", zap.Error(err))
		_ = a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "Failed to look up investigation.")
		return fmt.Errorf("get investigation: %w", err)
	}

	var fingerprint string
	for _, alertID := range inv.AlertIDs {
		alert, alertErr := a.alerts.GetByID(ctx, alertID)
		if alertErr != nil {
			continue
		}
		if alert.Fingerprint != "" {
			fingerprint = alert.Fingerprint
			break
		}
	}

	if fingerprint == "" {
		return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS,
			"No fingerprint found for this investigation's alerts.")
	}

	if a.suppressions == nil {
		return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS,
			"Suppression store not configured.")
	}

	expiresAt := time.Now().UTC().Add(1 * time.Hour)
	if err := a.suppressions.Create(ctx, fingerprint, expiresAt, action.UserID, "Suppressed via Slack button"); err != nil {
		a.logger.Error("failed to create suppression", zap.Error(err))
		_ = a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "Failed to create suppression.")
		return fmt.Errorf("create suppression: %w", err)
	}

	msg := fmt.Sprintf("Alerts with fingerprint `%s` will be suppressed for 1 hour.", fingerprint)
	return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, msg)
}

func (a *App) handleCompareDeployments(ctx context.Context, action transport.InteractionAction) error {
	investigationID := action.Value
	threadTS := action.MessageTS

	evidence, err := a.evidence.ListByInvestigation(ctx, investigationID)
	if err != nil {
		a.logger.Error("failed to fetch evidence for deploy comparison", zap.Error(err))
		_ = a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "Failed to retrieve evidence.")
		return fmt.Errorf("list evidence: %w", err)
	}

	var deployItems []contracts.Evidence
	for _, e := range evidence {
		if e.Kind == contracts.EvidenceDeploy || e.Kind == contracts.EvidenceGitChange {
			deployItems = append(deployItems, e)
		}
	}

	if len(deployItems) == 0 {
		return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS,
			"No deployment data available for this investigation.")
	}

	var sb strings.Builder
	sb.WriteString("*Deployment Comparison:*\n")

	for _, e := range deployItems {
		if e.Kind == contracts.EvidenceDeploy {
			sha := e.Attributes["sha"]
			env := e.Attributes["environment"]
			creator := e.Attributes["creator"]
			status := e.Attributes["status"]
			sb.WriteString(fmt.Sprintf("\n*Deploy:* `%s` to `%s` by %s (status: %s)\n",
				shortSHA(sha), env, creator, status))
		} else if e.Kind == contracts.EvidenceGitChange {
			filesChanged := e.Attributes["files_changed"]
			commitCount := e.Attributes["commit_count"]
			if filesChanged != "" {
				sb.WriteString(fmt.Sprintf("• %s files changed across %s commits\n", filesChanged, commitCount))
			}
			if msg := e.Attributes["message"]; msg != "" {
				sb.WriteString(fmt.Sprintf("• Commit: %s\n", truncateSlackText(firstLine(msg), 120)))
			} else {
				sb.WriteString(fmt.Sprintf("• %s\n", truncateSlackText(e.Summary, 200)))
			}
		}

		if sb.Len() > 2800 {
			sb.WriteString("_... truncated_\n")
			break
		}
	}

	return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, sb.String())
}

func (a *App) handleCreateIssue(ctx context.Context, action transport.InteractionAction) error {
	investigationID := action.Value
	threadTS := action.MessageTS

	inv, err := a.investigations.GetByID(ctx, investigationID)
	if err != nil {
		a.logger.Error("failed to fetch investigation for issue creation", zap.Error(err))
		_ = a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, "Failed to look up investigation.")
		return fmt.Errorf("get investigation: %w", err)
	}

	headline := inv.Headline
	if headline == "" {
		headline = "Investigation " + investigationID
	}

	narrative := "No hypothesis available."
	confidence := inv.Confidence
	var actionLines []string

	if len(inv.Hypotheses) > 0 {
		top := inv.Hypotheses[0]
		narrative = top.Narrative
		if top.Confidence > 0 {
			confidence = top.Confidence
		}
		for _, fix := range top.SuggestedFixes {
			actionLines = append(actionLines, fmt.Sprintf("- %s", fix.Title))
		}
	}

	actionsText := "_No recommended actions_"
	if len(actionLines) > 0 {
		actionsText = strings.Join(actionLines, "\n")
	}

	issueTemplate := fmt.Sprintf("*Issue Template (copy to create)*\n"+
		"*Title:* [Investigation] %s\n\n"+
		"*Body:*\n"+
		"## Root Cause\n%s\n\n"+
		"## Confidence: %.0f%%\n\n"+
		"## Recommended Actions\n%s",
		headline, narrative, confidence*100, actionsText)

	return a.publisher.PostEvidenceUpdate(ctx, action.ChannelID, threadTS, issueTemplate)
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
