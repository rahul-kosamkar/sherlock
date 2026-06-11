package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rahulkosamkar/sherlock/api"
	"github.com/rahulkosamkar/sherlock/internal/audit"
	"github.com/rahulkosamkar/sherlock/internal/collector"
	deploycollector "github.com/rahulkosamkar/sherlock/internal/collector/deploy"
	k8scollector "github.com/rahulkosamkar/sherlock/internal/collector/kubernetes"
	lokicollector "github.com/rahulkosamkar/sherlock/internal/collector/loki"
	promcollector "github.com/rahulkosamkar/sherlock/internal/collector/prometheus"
	"github.com/rahulkosamkar/sherlock/internal/config"
	"github.com/rahulkosamkar/sherlock/internal/tracing"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/correlation"
	"github.com/rahulkosamkar/sherlock/internal/dedup"
	"github.com/rahulkosamkar/sherlock/internal/entity"
	gitprovider "github.com/rahulkosamkar/sherlock/internal/git"
	"github.com/rahulkosamkar/sherlock/internal/httputil"
	"github.com/rahulkosamkar/sherlock/internal/investigation"
	"github.com/rahulkosamkar/sherlock/internal/llm"
	"github.com/rahulkosamkar/sherlock/internal/queue"
	"github.com/rahulkosamkar/sherlock/internal/rca"
	"github.com/rahulkosamkar/sherlock/internal/receiver"
	"github.com/rahulkosamkar/sherlock/internal/receiver/alertmanager"
	"github.com/rahulkosamkar/sherlock/internal/remediation"
	githubreceiver "github.com/rahulkosamkar/sherlock/internal/receiver/github"
	gitlabreceiver "github.com/rahulkosamkar/sherlock/internal/receiver/gitlab"
	"github.com/rahulkosamkar/sherlock/internal/receiver/grafana"
	sherlockslack "github.com/rahulkosamkar/sherlock/internal/slack"
	"github.com/rahulkosamkar/sherlock/internal/storage/objectstore"
	"github.com/rahulkosamkar/sherlock/internal/storage/postgres"
	"github.com/rahulkosamkar/sherlock/internal/timeline"
	"github.com/rahulkosamkar/sherlock/migrations"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("sherlock", version)
		return
	}

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		dsn := os.Getenv("SHERLOCK_POSTGRES_DSN")
		if dsn == "" {
			configPath := os.Getenv("SHERLOCK_CONFIG")
			if configPath == "" {
				configPath = "config.yaml"
			}
			cfg, cfgErr := config.Load(configPath)
			if cfgErr != nil {
				logger.Fatal("failed to load config for DSN", zap.Error(cfgErr))
			}
			dsn = cfg.Postgres.DSN
		}

		direction := "up"
		if len(os.Args) > 2 {
			direction = os.Args[2]
		}

		m, mErr := newMigrator(dsn)
		if mErr != nil {
			logger.Fatal("failed to create migrator", zap.Error(mErr))
		}

		switch direction {
		case "up":
			if err := m.Up(); err != nil && err != migrate.ErrNoChange {
				logger.Fatal("migrate up failed", zap.Error(err))
			}
			logger.Info("migrations applied successfully")
		case "down":
			if err := m.Down(); err != nil && err != migrate.ErrNoChange {
				logger.Fatal("migrate down failed", zap.Error(err))
			}
			logger.Info("migrations rolled back successfully")
		case "version":
			ver, dirty, err := m.Version()
			if err != nil {
				logger.Fatal("failed to get migration version", zap.Error(err))
			}
			logger.Info("current migration version", zap.Uint("version", ver), zap.Bool("dirty", dirty))
		default:
			logger.Fatal("unknown migrate subcommand", zap.String("subcommand", direction))
		}
		return
	}

	if err := run(logger); err != nil {
		logger.Fatal("failed to start sherlock", zap.Error(err))
	}
}

func run(logger *zap.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configPath := os.Getenv("SHERLOCK_CONFIG")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	tracingShutdown, tracingErr := tracing.Init(ctx, tracing.Config{
		Enabled:     cfg.Tracing.Enabled,
		Endpoint:    cfg.Tracing.Endpoint,
		ServiceName: "sherlock",
		Version:     version,
		SampleRate:  cfg.Tracing.SampleRate,
	})
	if tracingErr != nil {
		logger.Warn("failed to init tracing", zap.Error(tracingErr))
	} else {
		defer tracingShutdown(context.Background())
	}

	logger.Info("starting sherlock",
		zap.String("version", version),
		zap.String("address", cfg.Server.Address),
		zap.String("slack_mode", cfg.Slack.Mode),
		zap.Bool("tracing_enabled", cfg.Tracing.Enabled),
	)

	if cfg.AutoMigrate {
		logger.Info("auto-migrate enabled, applying migrations")
		m, mErr := newMigrator(cfg.Postgres.DSN)
		if mErr != nil {
			return fmt.Errorf("auto-migrate create migrator: %w", mErr)
		}
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			return fmt.Errorf("auto-migrate up: %w", err)
		}
		logger.Info("auto-migrate complete")
	}

	db, err := postgres.New(ctx, cfg.Postgres.DSN, cfg.Postgres.MaxConns)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer db.Close()

	objStore, err := objectstore.New(ctx,
		cfg.ObjectStore.Endpoint,
		cfg.ObjectStore.Bucket,
		cfg.ObjectStore.Region,
		cfg.ObjectStore.AccessKey,
		cfg.ObjectStore.SecretKey,
		cfg.ObjectStore.UsePathStyle,
	)
	if err != nil {
		return fmt.Errorf("init object store: %w", err)
	}

	q, err := queue.New(ctx, cfg.NATS.URL, cfg.NATS.StreamName)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer q.Close()

	investigationRepo := postgres.NewInvestigationRepo(db)
	evidenceRepo := postgres.NewEvidenceRepo(db)
	timelineRepo := postgres.NewTimelineRepo(db)
	hypothesisRepo := postgres.NewHypothesisRepo(db)
	alertRepo := postgres.NewAlertRepo(db)
	auditRepo := postgres.NewAuditRepo(db)
	suppressionRepo := postgres.NewSuppressionRepo(db)

	auditLogger := audit.NewLogger(auditRepo, logger)

	collectorRegistry := collector.NewRegistry(logger)

	k8sCol, err := k8scollector.New(cfg.Collectors.Kubernetes.Kubeconfig, cfg.Collectors.Kubernetes.InCluster, logger)
	if err != nil {
		logger.Warn("kubernetes collector not available", zap.Error(err))
	} else {
		collectorRegistry.Register(k8sCol)
	}

	promCol, err := promcollector.New(cfg.Collectors.Prometheus.URL, logger)
	if err != nil {
		logger.Warn("prometheus collector not available", zap.Error(err))
	} else {
		collectorRegistry.Register(promCol)
	}

	lokiCol, err := lokicollector.New(cfg.Collectors.Loki.URL, logger)
	if err != nil {
		logger.Warn("loki collector not available", zap.Error(err))
	} else {
		collectorRegistry.Register(lokiCol)
	}

	if cfg.Git.Enabled {
		deployCfg := deploycollector.Config{
			GitHubToken:   cfg.Git.Token,
			GitHubOrg:     cfg.Git.Organization,
			WorkloadRepos: cfg.Git.WorkloadRepos,
		}
		deployCol := deploycollector.New(deployCfg, logger)
		collectorRegistry.Register(deployCol)
	}

	entityResolver := entity.NewResolver()
	correlationEngine := correlation.New()
	rcaEngine := rca.New()
	timelineBuilder := timeline.New()

	var slackPublisher *sherlockslack.Publisher
	if cfg.Slack.BotToken != "" {
		slackPublisher = sherlockslack.NewPublisher(cfg.Slack.BotToken, logger)
	}

	orchestratorCfg := investigation.OrchestratorConfig{
		MaxConcurrent: cfg.Investigation.MaxConcurrent,
		Timeout:       cfg.Investigation.Timeout,
		StreamName:    cfg.NATS.StreamName,
		LLMEnabled:    cfg.LLM.Enabled,
	}

	entityAdapter := &entityResolverAdapter{resolver: entityResolver}

	orch := investigation.NewOrchestrator(
		orchestratorCfg,
		investigationRepo,
		evidenceRepo,
		timelineRepo,
		hypothesisRepo,
		alertRepo,
		collectorRegistry,
		correlationEngine,
		rcaEngine,
		timelineBuilder,
		entityAdapter,
		slackPublisher,
		auditLogger,
		q,
		logger,
	)

	if cfg.Remediation.Enabled {
		remEngine := remediation.New(logger)
		if cfg.Remediation.PoliciesPath != "" {
			if err := remEngine.LoadPolicies(cfg.Remediation.PoliciesPath); err != nil {
				logger.Warn("failed to load remediation policies, using defaults", zap.Error(err))
				remEngine.LoadDefaults()
			}
		} else {
			remEngine.LoadDefaults()
		}
		orch.SetRemediation(remEngine)
		logger.Info("remediation engine enabled")
	}

	if cfg.LLM.Enabled {
		llmProvider, llmErr := llm.NewProvider(llm.ProviderConfig{
			Provider:    cfg.LLM.Provider,
			Model:       cfg.LLM.Model,
			APIKey:      cfg.LLM.APIKey,
			Endpoint:    cfg.LLM.Endpoint,
			GCPProject:  cfg.LLM.GCPProject,
			GCPRegion:   cfg.LLM.GCPRegion,
			Temperature: cfg.LLM.Temperature,
			MaxTokens:   cfg.LLM.MaxTokens,
			Timeout:     cfg.LLM.Timeout,
		})
		if llmErr != nil {
			logger.Error("failed to initialize LLM provider, falling back to rule-based RCA", zap.Error(llmErr))
		} else {
			var gitProv gitprovider.Provider
			if cfg.Git.Enabled {
				gitProv = gitprovider.NewGitHubProvider(gitprovider.Config{
					Provider:      cfg.Git.Provider,
					Token:         cfg.Git.Token,
					Organization:  cfg.Git.Organization,
					DefaultBranch: cfg.Git.DefaultBranch,
					WorkloadRepos: cfg.Git.WorkloadRepos,
				}, logger)
			} else {
				gitProv = gitprovider.NewNoopProvider()
			}

			followUpExec := llm.NewFollowUpExecutor(collectorRegistry, gitProv, logger)
			llmEngine := rca.NewLLMEngine(llmProvider, followUpExec, rcaEngine, rca.LLMEngineConfig{
				MaxPasses: cfg.LLM.MaxPasses,
			}, logger)

			if slackPublisher != nil {
				llmEngine.SetNotifier(slackPublisher)
			}

			orch.SetLLMEngine(llmEngine)
			logger.Info("LLM-powered RCA engine enabled",
				zap.String("provider", cfg.LLM.Provider),
				zap.String("model", cfg.LLM.Model),
				zap.Int("max_passes", cfg.LLM.MaxPasses),
				zap.Bool("git_enabled", cfg.Git.Enabled),
			)
		}
	}

	go func() {
		if err := orch.Start(ctx, cfg.NATS.StreamName); err != nil {
			logger.Error("orchestrator failed", zap.Error(err))
		}
	}()

	gateway := receiver.NewGateway(q, objStore, logger)
	rl := httputil.NewIPRateLimiter(rate.Limit(cfg.Server.RateLimitRPS), cfg.Server.RateLimitBurst)
	gateway.SetRateLimiter(rl.Middleware)
	if cfg.Receivers.Grafana.Enabled {
		gateway.Register(grafana.New(cfg.Receivers.Grafana.HMACSecret))
	}
	if cfg.Receivers.Alertmanager.Enabled {
		gateway.Register(alertmanager.New())
	}

	if cfg.Dedup.Enabled {
		dedupSvc := dedup.New(investigationRepo, cfg.Dedup.Window, logger)
		gateway.SetDedup(&dedupCheckerAdapter{svc: dedupSvc})
		if slackPublisher != nil {
			gateway.SetDedupNotifier(slackPublisher)
		}
		logger.Info("dedup enabled", zap.Duration("window", cfg.Dedup.Window))
	}

	gateway.SetSuppress(suppressionRepo)
	if cfg.Receivers.GitHub.Enabled {
		gateway.Register(githubreceiver.New(cfg.Receivers.GitHub.WebhookSecret))
	}
	if cfg.Receivers.GitLab.Enabled {
		gateway.Register(gitlabreceiver.New(cfg.Receivers.GitLab.SecretToken))
	}

	apiServer := api.NewServer(investigationRepo, evidenceRepo, timelineRepo, hypothesisRepo, logger)
	if cfg.Server.APIKey != "" {
		apiServer.SetAPIKeyAuth(httputil.APIKeyAuth(cfg.Server.APIKey))
	}

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.RealIP)
	router.Mount("/", gateway.Routes())
	router.Mount("/", apiServer.Routes())
	router.Handle("/metrics", promhttp.Handler())

	var slackApp *sherlockslack.App
	if cfg.Slack.BotToken != "" {
		enqueuer := &slackEnqueuer{publisher: q, streamName: cfg.NATS.StreamName}
		slackCfg := sherlockslack.AppConfig{
			BotToken:      cfg.Slack.BotToken,
			AppToken:      cfg.Slack.AppToken,
			SigningSecret: cfg.Slack.SigningSecret,
			Mode:          cfg.Slack.Mode,
			HTTPAddress:   cfg.Server.Address,
		}
		slackApp, err = sherlockslack.NewApp(slackCfg, enqueuer, slackPublisher, evidenceRepo, alertRepo, investigationRepo, suppressionRepo, logger)
		if err != nil {
			return fmt.Errorf("init slack app: %w", err)
		}
		go func() {
			if err := slackApp.Start(ctx); err != nil {
				logger.Error("slack app failed", zap.Error(err))
			}
		}()
	}

	srv := &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", zap.String("address", cfg.Server.Address))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutting down", zap.String("signal", sig.String()))
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if slackApp != nil {
		slackApp.Stop()
	}
	orch.Stop()
	cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	logger.Info("sherlock stopped")
	return nil
}

type entityResolverAdapter struct {
	resolver *entity.Resolver
}

func (a *entityResolverAdapter) Resolve(alert *contracts.NormalizedAlert) investigation.EntityResolveResult {
	result := a.resolver.Resolve(alert)
	return investigation.EntityResolveResult{
		Targets:  result.Targets,
		TimeFrom: result.TimeFrom,
		TimeTo:   result.TimeTo,
	}
}

type slackEnqueuer struct {
	publisher investigation.QueuePublisher
	streamName string
}

func (e *slackEnqueuer) EnqueueInvestigation(ctx context.Context, channelID, threadTS, userID, text string) (string, error) {
	alert := contracts.NormalizedAlert{
		Title:   text,
		Summary: fmt.Sprintf("Investigation requested by user in Slack: %s", text),
		Status:  contracts.AlertStatusFiring,
		Labels:  parseLabelsFromText(text),
	}

	return investigation.EnqueueInvestigation(ctx, e.publisher, e.streamName, alert, channelID, threadTS, userID)
}

func parseLabelsFromText(text string) map[string]string {
	labels := make(map[string]string)
	labels["source"] = "slack"
	if text != "" {
		labels["service"] = text
	}
	return labels
}

type dedupCheckerAdapter struct {
	svc *dedup.Service
}

func (a *dedupCheckerAdapter) Check(ctx context.Context, alert contracts.NormalizedAlert) (*receiver.DedupResult, error) {
	result, err := a.svc.Check(ctx, alert)
	if err != nil {
		return nil, err
	}
	return &receiver.DedupResult{
		IsDuplicate:      result.IsDuplicate,
		ExistingID:       result.ExistingID,
		ExistingHeadline: result.ExistingHeadline,
		ExistingChannel:  result.ExistingChannel,
		ExistingThread:   result.ExistingThread,
	}, nil
}

func newMigrator(dsn string) (*migrate.Migrate, error) {
	d, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("iofs source: %w", err)
	}
	dbURL := dsn
	if len(dbURL) > 8 && dbURL[:8] == "postgres" {
		dbURL = "pgx5" + dbURL[8:]
	}
	m, err := migrate.NewWithSourceInstance("iofs", d, dbURL)
	if err != nil {
		return nil, fmt.Errorf("new migrate instance: %w", err)
	}
	return m, nil
}
