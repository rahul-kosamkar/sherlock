package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/entity"
)

type mockPublisher struct {
	subject string
	data    []byte
	err     error
}

func (m *mockPublisher) Publish(_ context.Context, subject string, data []byte) error {
	m.subject = subject
	m.data = data
	return m.err
}

type testJob struct {
	Alert          contracts.NormalizedAlert `json:"alert"`
	SlackChannelID string                    `json:"slack_channel_id,omitempty"`
	SlackThreadTS  string                    `json:"slack_thread_ts,omitempty"`
	RequestedBy    string                    `json:"requested_by,omitempty"`
}

func TestParseLabelsFromText_WithText(t *testing.T) {
	t.Parallel()
	labels := parseLabelsFromText("payment-api")

	if got := labels["source"]; got != "slack" {
		t.Errorf("source: want %q, got %q", "slack", got)
	}
	if got := labels["service"]; got != "payment-api" {
		t.Errorf("service: want %q, got %q", "payment-api", got)
	}
}

func TestParseLabelsFromText_EmptyText(t *testing.T) {
	t.Parallel()
	labels := parseLabelsFromText("")

	if got := labels["source"]; got != "slack" {
		t.Errorf("source: want %q, got %q", "slack", got)
	}
	if _, ok := labels["service"]; ok {
		t.Error("expected no service key for empty text")
	}
}

func TestEntityResolverAdapter_Resolve(t *testing.T) {
	t.Parallel()
	resolver := entity.NewResolver()
	adapter := &entityResolverAdapter{resolver: resolver}

	alert := &contracts.NormalizedAlert{
		Title:    "test alert",
		Status:   contracts.AlertStatusFiring,
		StartsAt: time.Now().Add(-10 * time.Minute),
		Labels: map[string]string{
			"service":   "test",
			"namespace": "prod",
		},
	}

	result := adapter.Resolve(alert)
	if len(result.Targets) == 0 {
		t.Fatal("expected non-empty Targets")
	}
}

func TestSlackEnqueuer_Success(t *testing.T) {
	t.Parallel()
	pub := &mockPublisher{}
	streamName := "sherlock-test"
	enqueuer := &slackEnqueuer{publisher: pub, streamName: streamName}

	_, err := enqueuer.EnqueueInvestigation(context.Background(), "C123", "ts123", "U456", "payment-api down")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := streamName + ".new"
	if pub.subject != want {
		t.Errorf("subject: want %q, got %q", want, pub.subject)
	}
}

func TestSlackEnqueuer_SetsAlert(t *testing.T) {
	t.Parallel()
	pub := &mockPublisher{}
	enqueuer := &slackEnqueuer{publisher: pub, streamName: "investigations"}

	text := "payment-api is broken"
	_, err := enqueuer.EnqueueInvestigation(context.Background(), "C1", "ts1", "U1", text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var job testJob
	if err := json.Unmarshal(pub.data, &job); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}
	if job.Alert.Title != text {
		t.Errorf("Title: want %q, got %q", text, job.Alert.Title)
	}
	if job.Alert.Status != contracts.AlertStatusFiring {
		t.Errorf("Status: want %q, got %q", contracts.AlertStatusFiring, job.Alert.Status)
	}
	if job.Alert.Labels["source"] != "slack" {
		t.Errorf("source label: want %q, got %q", "slack", job.Alert.Labels["source"])
	}
}

func TestSlackEnqueuer_PublishError(t *testing.T) {
	t.Parallel()
	pub := &mockPublisher{err: fmt.Errorf("nats connection lost")}
	enqueuer := &slackEnqueuer{publisher: pub, streamName: "investigations"}

	_, err := enqueuer.EnqueueInvestigation(context.Background(), "C1", "ts1", "U1", "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
