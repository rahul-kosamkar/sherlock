//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/storage/postgres"
)

var testDB *postgres.DB

func TestMain(m *testing.M) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "sherlock",
			"POSTGRES_PASSWORD": "sherlock",
			"POSTGRES_DB":       "sherlock_test",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}

	pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start postgres container: %v\n", err)
		os.Exit(1)
	}

	host, _ := pgContainer.Host(ctx)
	port, _ := pgContainer.MappedPort(ctx, "5432")
	dsn := fmt.Sprintf("postgres://sherlock:sherlock@%s:%s/sherlock_test?sslmode=disable", host, port.Port())

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to postgres: %v\n", err)
		os.Exit(1)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")

	for _, mig := range []string{"001_initial.up.sql", "002_v1_dedup.up.sql"} {
		sql, err := os.ReadFile(filepath.Join(migrationsDir, mig))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read migration %s: %v\n", mig, err)
			os.Exit(1)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			fmt.Fprintf(os.Stderr, "failed to apply migration %s: %v\n", mig, err)
			os.Exit(1)
		}
	}

	testDB, err = postgres.New(ctx, dsn, 5)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create DB: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	testDB.Close()
	pool.Close()
	_ = pgContainer.Terminate(ctx)
	os.Exit(code)
}

func newInvestigation(tenantID string) *contracts.Investigation {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &contracts.Investigation{
		ID:       uuid.NewString(),
		TenantID: tenantID,
		Status:   contracts.StatusPending,
		AlertIDs: []string{uuid.NewString()},
		Targets: []contracts.TargetRef{
			{Kind: "service", Name: "api-gateway"},
		},
		TimeFrom:   now.Add(-1 * time.Hour),
		TimeTo:     now,
		Headline:   "test investigation",
		Confidence: 0.5,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func newAlert(tenantID string) *contracts.NormalizedAlert {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &contracts.NormalizedAlert{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		Source:      "grafana",
		Status:      contracts.AlertStatusFiring,
		Severity:    contracts.SeverityCritical,
		Title:       "CPU High",
		Summary:     "CPU usage exceeded 90%",
		Fingerprint: "fp-" + uuid.NewString()[:8],
		GroupKey:     "group-1",
		StartsAt:    now,
		Labels:      map[string]string{"env": "prod"},
		Annotations: map[string]string{"dashboard": "https://grafana.example.com"},
		EntityHints: []contracts.TargetRef{{Kind: "service", Name: "api"}},
		Links:       []contracts.Link{{Rel: "dashboard", Href: "https://grafana.example.com/d/123"}},
		RawRef:      "raw/grafana/2024-01-01/" + uuid.NewString(),
	}
}

// --- InvestigationRepo ---

func TestInvestigationRepo_CreateAndGetByID(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)
	inv := newInvestigation("tenant-1")

	if err := repo.Create(ctx, inv); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if got.ID != inv.ID {
		t.Errorf("ID: got %q, want %q", got.ID, inv.ID)
	}
	if got.TenantID != inv.TenantID {
		t.Errorf("TenantID: got %q, want %q", got.TenantID, inv.TenantID)
	}
	if got.Status != inv.Status {
		t.Errorf("Status: got %q, want %q", got.Status, inv.Status)
	}
	if got.Headline != inv.Headline {
		t.Errorf("Headline: got %q, want %q", got.Headline, inv.Headline)
	}
	if len(got.Targets) != 1 || got.Targets[0].Name != "api-gateway" {
		t.Errorf("Targets: got %v, want [{service api-gateway}]", got.Targets)
	}
}

func TestInvestigationRepo_UpdateStatus(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)
	inv := newInvestigation("tenant-2")

	if err := repo.Create(ctx, inv); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.UpdateStatus(ctx, inv.ID, contracts.StatusCollecting); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.GetByID(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != contracts.StatusCollecting {
		t.Errorf("Status: got %q, want %q", got.Status, contracts.StatusCollecting)
	}
}

func TestInvestigationRepo_Complete(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)
	inv := newInvestigation("tenant-3")

	if err := repo.Create(ctx, inv); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Complete(ctx, inv.ID, "root cause found", 0.95); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := repo.GetByID(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != contracts.StatusDone {
		t.Errorf("Status: got %q, want %q", got.Status, contracts.StatusDone)
	}
	if got.Headline != "root cause found" {
		t.Errorf("Headline: got %q, want %q", got.Headline, "root cause found")
	}
	if got.Confidence != 0.95 {
		t.Errorf("Confidence: got %f, want 0.95", got.Confidence)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should be non-nil after Complete")
	}
}

func TestInvestigationRepo_FindActiveByFingerprint(t *testing.T) {
	ctx := context.Background()
	invRepo := postgres.NewInvestigationRepo(testDB)
	alertRepo := postgres.NewAlertRepo(testDB)

	alert := newAlert("tenant-fp")
	if err := alertRepo.Create(ctx, alert); err != nil {
		t.Fatalf("Create alert: %v", err)
	}

	inv := newInvestigation("tenant-fp")
	inv.AlertIDs = []string{alert.ID}
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatalf("Create investigation: %v", err)
	}

	got, err := invRepo.FindActiveByFingerprint(ctx, alert.Fingerprint, inv.CreatedAt.Add(-time.Minute))
	if err != nil {
		t.Fatalf("FindActiveByFingerprint: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil active investigation")
	}
	if got.ID != inv.ID {
		t.Errorf("ID: got %q, want %q", got.ID, inv.ID)
	}
}

func TestInvestigationRepo_LinkAlertToInvestigation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)
	inv := newInvestigation("tenant-link")

	if err := repo.Create(ctx, inv); err != nil {
		t.Fatalf("Create: %v", err)
	}

	newAlertID := uuid.NewString()
	if err := repo.LinkAlertToInvestigation(ctx, inv.ID, newAlertID); err != nil {
		t.Fatalf("LinkAlertToInvestigation: %v", err)
	}

	got, err := repo.GetByID(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	found := false
	for _, id := range got.AlertIDs {
		if id == newAlertID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("linked alert %q not found in AlertIDs %v", newAlertID, got.AlertIDs)
	}
}

// --- AlertRepo ---

func TestAlertRepo_CreateAndGetByID(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewAlertRepo(testDB)
	alert := newAlert("tenant-alert-1")

	if err := repo.Create(ctx, alert); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, alert.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != alert.ID {
		t.Errorf("ID: got %q, want %q", got.ID, alert.ID)
	}
	if got.Title != alert.Title {
		t.Errorf("Title: got %q, want %q", got.Title, alert.Title)
	}
	if got.Fingerprint != alert.Fingerprint {
		t.Errorf("Fingerprint: got %q, want %q", got.Fingerprint, alert.Fingerprint)
	}
}

func TestAlertRepo_GetByFingerprint(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewAlertRepo(testDB)
	alert := newAlert("tenant-alert-fp")

	if err := repo.Create(ctx, alert); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByFingerprint(ctx, alert.TenantID, alert.Fingerprint)
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if got.ID != alert.ID {
		t.Errorf("ID: got %q, want %q", got.ID, alert.ID)
	}
}

func TestAlertRepo_GetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewAlertRepo(testDB)

	_, err := repo.GetByID(ctx, uuid.NewString())
	if err == nil {
		t.Fatal("expected error for non-existent alert")
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// --- EvidenceRepo ---

func TestEvidenceRepo_CreateBatchAndList(t *testing.T) {
	ctx := context.Background()
	invRepo := postgres.NewInvestigationRepo(testDB)
	evRepo := postgres.NewEvidenceRepo(testDB)

	inv := newInvestigation("tenant-ev")
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatalf("Create investigation: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	evidence := []contracts.Evidence{
		{
			ID:              uuid.NewString(),
			InvestigationID: inv.ID,
			Kind:            contracts.EvidenceLog,
			Source:          "loki",
			Target:          contracts.TargetRef{Kind: "service", Name: "api"},
			CollectedAt:     now,
			ObservedAtFrom:  now.Add(-30 * time.Minute),
			ObservedAtTo:    now,
			Summary:         "error logs spike",
			BodyRef:         "s3://evidence/log1",
			Query:           `{job="api"} |= "error"`,
			Score:           0.8,
			Attributes:      map[string]string{"log_count": "150"},
			RedactionState:  contracts.RedactionNone,
		},
		{
			ID:              uuid.NewString(),
			InvestigationID: inv.ID,
			Kind:            contracts.EvidenceMetric,
			Source:          "prometheus",
			Target:          contracts.TargetRef{Kind: "service", Name: "api"},
			CollectedAt:     now,
			ObservedAtFrom:  now.Add(-30 * time.Minute),
			ObservedAtTo:    now,
			Summary:         "latency p99 spike",
			BodyRef:         "s3://evidence/metric1",
			Query:           `histogram_quantile(0.99, rate(http_duration_seconds_bucket[5m]))`,
			Score:           0.7,
			Attributes:      map[string]string{"value": "2.5s"},
			RedactionState:  contracts.RedactionNone,
		},
	}

	if err := evRepo.CreateBatch(ctx, evidence); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	got, err := evRepo.ListByInvestigation(ctx, inv.ID)
	if err != nil {
		t.Fatalf("ListByInvestigation: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 evidence items, got %d", len(got))
	}
}

// --- HypothesisRepo ---

func TestHypothesisRepo_CreateBatchAndList(t *testing.T) {
	ctx := context.Background()
	invRepo := postgres.NewInvestigationRepo(testDB)
	hypoRepo := postgres.NewHypothesisRepo(testDB)

	inv := newInvestigation("tenant-hypo")
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatalf("Create investigation: %v", err)
	}

	hypotheses := []contracts.Hypothesis{
		{
			ID:            uuid.NewString(),
			Title:         "Bad deploy",
			Narrative:     "Deploy v2.3.1 introduced a memory leak",
			CauseCategory: contracts.CauseDeploy,
			Confidence:    0.85,
			Supporting:    []string{"ev-1"},
			Contradicting: []string{},
			SuggestedFixes: []contracts.SuggestedFix{
				{Title: "Rollback", Description: "Rollback to v2.3.0", SafeByDefault: true},
			},
		},
		{
			ID:            uuid.NewString(),
			Title:         "Capacity",
			Narrative:     "Insufficient replicas",
			CauseCategory: contracts.CauseCapacity,
			Confidence:    0.4,
			Supporting:    []string{},
			Contradicting: []string{"ev-2"},
			SuggestedFixes: []contracts.SuggestedFix{
				{Title: "Scale up", Description: "Increase replicas to 5"},
			},
		},
	}

	if err := hypoRepo.CreateBatch(ctx, inv.ID, hypotheses); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	got, err := hypoRepo.ListByInvestigation(ctx, inv.ID)
	if err != nil {
		t.Fatalf("ListByInvestigation: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 hypotheses, got %d", len(got))
	}
	if got[0].Confidence < got[1].Confidence {
		t.Error("expected hypotheses ordered by confidence DESC")
	}
}

// --- TimelineRepo ---

func TestTimelineRepo_CreateBatchAndList(t *testing.T) {
	ctx := context.Background()
	invRepo := postgres.NewInvestigationRepo(testDB)
	tlRepo := postgres.NewTimelineRepo(testDB)

	inv := newInvestigation("tenant-tl")
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatalf("Create investigation: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	events := []contracts.TimelineEvent{
		{
			ID:              uuid.NewString(),
			InvestigationID: inv.ID,
			Timestamp:       now.Add(-10 * time.Minute),
			Kind:            contracts.TimelineDeploy,
			Source:          "github",
			Narrative:       "Deploy v2.3.1",
			EvidenceIDs:     []string{"ev-1"},
			Attributes:      map[string]string{"commit": "abc123"},
		},
		{
			ID:              uuid.NewString(),
			InvestigationID: inv.ID,
			Timestamp:       now.Add(-5 * time.Minute),
			Kind:            contracts.TimelineAlert,
			Source:          "grafana",
			Narrative:       "CPU alert fired",
			EvidenceIDs:     []string{"ev-2"},
			Attributes:      map[string]string{"severity": "critical"},
		},
	}

	if err := tlRepo.CreateBatch(ctx, events); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	got, err := tlRepo.ListByInvestigation(ctx, inv.ID)
	if err != nil {
		t.Fatalf("ListByInvestigation: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 timeline events, got %d", len(got))
	}
	if !got[0].Timestamp.Before(got[1].Timestamp) {
		t.Error("expected timeline events ordered by timestamp ASC")
	}
}

// --- AuditRepo ---

func TestAuditRepo_CreateAndList(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewAuditRepo(testDB)
	tenantID := "tenant-audit-" + uuid.NewString()[:8]

	now := time.Now().UTC().Truncate(time.Microsecond)
	entries := []*contracts.AuditEntry{
		{
			ID:        uuid.NewString(),
			TenantID:  tenantID,
			Actor:     "system",
			Action:    "investigation.created",
			Target:    "inv-123",
			Metadata:  map[string]string{"source": "grafana"},
			Timestamp: now.Add(-5 * time.Minute),
		},
		{
			ID:        uuid.NewString(),
			TenantID:  tenantID,
			Actor:     "system",
			Action:    "investigation.completed",
			Target:    "inv-123",
			Metadata:  map[string]string{"confidence": "0.9"},
			Timestamp: now,
		},
	}

	for _, e := range entries {
		if err := repo.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := repo.List(ctx, tenantID, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(got))
	}
	if got[0].Timestamp.Before(got[1].Timestamp) {
		t.Error("expected audit entries ordered by timestamp DESC")
	}
}

// --- SuppressionRepo ---

func TestSuppressionRepo_CreateAndIsActive(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewSuppressionRepo(testDB)
	fp := "fp-supp-" + uuid.NewString()[:8]

	if err := repo.Create(ctx, fp, time.Now().Add(1*time.Hour), "admin", "known flaky"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	active, err := repo.IsActive(ctx, fp)
	if err != nil {
		t.Fatalf("IsActive: %v", err)
	}
	if !active {
		t.Error("expected suppression to be active")
	}
}

func TestSuppressionRepo_Expired(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewSuppressionRepo(testDB)
	fp := "fp-supp-exp-" + uuid.NewString()[:8]

	if err := repo.Create(ctx, fp, time.Now().Add(-1*time.Hour), "admin", "already expired"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	active, err := repo.IsActive(ctx, fp)
	if err != nil {
		t.Fatalf("IsActive: %v", err)
	}
	if active {
		t.Error("expected expired suppression to be inactive")
	}
}

// --- ErrNotFound sentinel ---

func TestErrNotFound_InvestigationGetByID(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)

	_, err := repo.GetByID(ctx, uuid.NewString())
	if err == nil {
		t.Fatal("expected error for non-existent investigation")
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestErrNotFound_InvestigationUpdateStatus(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)

	err := repo.UpdateStatus(ctx, uuid.NewString(), contracts.StatusCollecting)
	if err == nil {
		t.Fatal("expected error for non-existent investigation")
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestErrNotFound_InvestigationComplete(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)

	err := repo.Complete(ctx, uuid.NewString(), "headline", 0.5)
	if err == nil {
		t.Fatal("expected error for non-existent investigation")
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestErrNotFound_InvestigationLinkAlert(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)

	err := repo.LinkAlertToInvestigation(ctx, uuid.NewString(), uuid.NewString())
	if err == nil {
		t.Fatal("expected error for non-existent investigation")
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// --- Transactional pattern ---

func TestTransactionalPattern_CommitVisible(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)

	tx, err := testDB.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	txRepo := repo.WithTx(tx)
	inv := newInvestigation("tenant-tx-commit")

	if err := txRepo.Create(ctx, inv); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Create in tx: %v", err)
	}

	got, err := txRepo.GetByID(ctx, inv.ID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("GetByID within tx: %v", err)
	}
	if got.ID != inv.ID {
		_ = tx.Rollback(ctx)
		t.Fatalf("expected investigation visible within tx")
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err = repo.GetByID(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetByID after commit: %v", err)
	}
	if got.ID != inv.ID {
		t.Error("expected investigation visible after commit")
	}
}

func TestTransactionalPattern_RollbackInvisible(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewInvestigationRepo(testDB)

	tx, err := testDB.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	txRepo := repo.WithTx(tx)
	inv := newInvestigation("tenant-tx-rollback")

	if err := txRepo.Create(ctx, inv); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Create in tx: %v", err)
	}

	got, err := txRepo.GetByID(ctx, inv.ID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("GetByID within tx: %v", err)
	}
	if got.ID != inv.ID {
		_ = tx.Rollback(ctx)
		t.Fatal("expected investigation visible within tx before rollback")
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	_, err = repo.GetByID(ctx, inv.ID)
	if err == nil {
		t.Fatal("expected error: investigation should not be visible after rollback")
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		t.Errorf("expected ErrNotFound after rollback, got: %v", err)
	}
}
