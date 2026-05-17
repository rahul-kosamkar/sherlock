package entity

import (
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

func TestResolver_Resolve_WithEntityHints(t *testing.T) {
	hints := []contracts.TargetRef{
		{Kind: "service", Name: "payment", Namespace: "prod"},
	}
	alert := &contracts.NormalizedAlert{
		StartsAt:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		EntityHints: hints,
	}

	result := NewResolver().Resolve(alert)

	if len(result.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(result.Targets))
	}
	if result.Targets[0].Name != "payment" {
		t.Errorf("expected target name %q, got %q", "payment", result.Targets[0].Name)
	}
}

func TestResolver_Resolve_InfersFromLabels(t *testing.T) {
	alert := &contracts.NormalizedAlert{
		StartsAt: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Labels:   map[string]string{"service": "order-api", "namespace": "prod"},
	}

	result := NewResolver().Resolve(alert)

	if len(result.Targets) == 0 {
		t.Fatal("expected at least 1 inferred target")
	}
	if result.Targets[0].Name != "order-api" {
		t.Errorf("expected inferred name %q, got %q", "order-api", result.Targets[0].Name)
	}
}

func TestResolver_InferFromLabels_ServiceLabel(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{"service": "payment", "namespace": "prod"}
	targets := r.inferFromLabels(labels)

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Kind != "service" {
		t.Errorf("expected kind %q, got %q", "service", targets[0].Kind)
	}
	if targets[0].Name != "payment" {
		t.Errorf("expected name %q, got %q", "payment", targets[0].Name)
	}
}

func TestResolver_InferFromLabels_AppLabel(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{"app": "order-api"}
	targets := r.inferFromLabels(labels)

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Name != "order-api" {
		t.Errorf("expected name %q, got %q", "order-api", targets[0].Name)
	}
}

func TestResolver_InferFromLabels_K8sAppLabel(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{"app.kubernetes.io/name": "foo"}
	targets := r.inferFromLabels(labels)

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Name != "foo" {
		t.Errorf("expected name %q, got %q", "foo", targets[0].Name)
	}
}

func TestResolver_InferFromLabels_Deployment(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{"deployment": "web", "namespace": "prod"}
	targets := r.inferFromLabels(labels)

	found := false
	for _, tgt := range targets {
		if tgt.Kind == "k8s.deployment" && tgt.Name == "web" && tgt.Namespace == "prod" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected k8s.deployment target with name=web, namespace=prod; got %+v", targets)
	}
}

func TestResolver_InferFromLabels_Pod(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{"pod": "web-abc123", "namespace": "prod"}
	targets := r.inferFromLabels(labels)

	found := false
	for _, tgt := range targets {
		if tgt.Kind == "k8s.pod" && tgt.Name == "web-abc123" && tgt.Namespace == "prod" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected k8s.pod target with name=web-abc123; got %+v", targets)
	}
}

func TestResolver_InferFromLabels_NoDeployWithoutNamespace(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{"deployment": "web"}
	targets := r.inferFromLabels(labels)

	for _, tgt := range targets {
		if tgt.Kind == "k8s.deployment" {
			t.Errorf("did not expect k8s.deployment target without namespace; got %+v", tgt)
		}
	}
}

func TestResolver_InferFromLabels_ClusterAndEnv(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{
		"service":     "payment",
		"cluster":     "us-east",
		"environment": "prod",
	}
	targets := r.inferFromLabels(labels)

	if len(targets) == 0 {
		t.Fatal("expected at least 1 target")
	}
	if targets[0].Cluster != "us-east" {
		t.Errorf("expected cluster %q, got %q", "us-east", targets[0].Cluster)
	}
	if targets[0].Environment != "prod" {
		t.Errorf("expected environment %q, got %q", "prod", targets[0].Environment)
	}
}

func TestResolver_InferFromLabels_EmptyLabels(t *testing.T) {
	r := NewResolver()
	targets := r.inferFromLabels(map[string]string{})
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for empty labels, got %d", len(targets))
	}
}

func TestResolver_InferFromLabels_MultipleTargets(t *testing.T) {
	r := NewResolver()
	labels := map[string]string{
		"service":    "payment",
		"deployment": "web",
		"pod":        "web-abc123",
		"namespace":  "prod",
	}
	targets := r.inferFromLabels(labels)

	kinds := make(map[string]bool)
	for _, tgt := range targets {
		kinds[tgt.Kind] = true
	}
	for _, want := range []string{"service", "k8s.deployment", "k8s.pod"} {
		if !kinds[want] {
			t.Errorf("expected target kind %q in results", want)
		}
	}
}

func TestResolver_TimeWindow_Default(t *testing.T) {
	startsAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	alert := &contracts.NormalizedAlert{StartsAt: startsAt}

	result := NewResolver().Resolve(alert)

	expectedFrom := startsAt.Add(-30 * time.Minute)
	if !result.TimeFrom.Equal(expectedFrom) {
		t.Errorf("expected TimeFrom = %v, got %v", expectedFrom, result.TimeFrom)
	}
	if result.TimeTo.Before(startsAt) {
		t.Errorf("expected TimeTo >= StartsAt, got %v", result.TimeTo)
	}
}

func TestResolver_TimeWindow_WithEndsAt(t *testing.T) {
	startsAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	endsAt := time.Date(2025, 1, 1, 13, 0, 0, 0, time.UTC)
	alert := &contracts.NormalizedAlert{StartsAt: startsAt, EndsAt: &endsAt}

	result := NewResolver().Resolve(alert)

	if !result.TimeTo.Equal(endsAt) {
		t.Errorf("expected TimeTo = %v, got %v", endsAt, result.TimeTo)
	}
}

func TestResolver_Dedup(t *testing.T) {
	dup := contracts.TargetRef{Kind: "service", Name: "payment", Namespace: "prod"}
	alert := &contracts.NormalizedAlert{
		StartsAt:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		EntityHints: []contracts.TargetRef{dup, dup, dup},
	}

	result := NewResolver().Resolve(alert)
	if len(result.Targets) != 1 {
		t.Errorf("expected 1 deduplicated target, got %d", len(result.Targets))
	}
}

func TestFirstLabel_Found(t *testing.T) {
	labels := map[string]string{"service": "payment"}
	if got := firstLabel(labels, "service"); got != "payment" {
		t.Errorf("expected %q, got %q", "payment", got)
	}
}

func TestFirstLabel_FallbackOrder(t *testing.T) {
	labels := map[string]string{"app": "order-api"}
	if got := firstLabel(labels, "service", "app"); got != "order-api" {
		t.Errorf("expected %q, got %q", "order-api", got)
	}
}

func TestFirstLabel_NotFound(t *testing.T) {
	labels := map[string]string{"unrelated": "value"}
	if got := firstLabel(labels, "service", "app"); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
