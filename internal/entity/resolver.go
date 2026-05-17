package entity

import (
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type ResolveResult struct {
	Targets  []contracts.TargetRef
	TimeFrom time.Time
	TimeTo   time.Time
}

type Resolver struct{}

func NewResolver() *Resolver {
	return &Resolver{}
}

func (r *Resolver) Resolve(alert *contracts.NormalizedAlert) ResolveResult {
	targets := r.resolveTargets(alert)
	from, to := r.resolveTimeWindow(alert)
	return ResolveResult{
		Targets:  targets,
		TimeFrom: from,
		TimeTo:   to,
	}
}

func (r *Resolver) resolveTargets(alert *contracts.NormalizedAlert) []contracts.TargetRef {
	if len(alert.EntityHints) > 0 {
		return dedup(alert.EntityHints)
	}
	return dedup(r.inferFromLabels(alert.Labels))
}

func (r *Resolver) inferFromLabels(labels map[string]string) []contracts.TargetRef {
	var targets []contracts.TargetRef

	cluster := firstLabel(labels, "cluster")
	env := firstLabel(labels, "environment", "env")
	ns := labels["namespace"]

	if name := firstLabel(labels, "service", "app", "app.kubernetes.io/name"); name != "" {
		targets = append(targets, contracts.TargetRef{
			Kind:        "service",
			Namespace:   ns,
			Name:        name,
			Cluster:     cluster,
			Environment: env,
		})
	}

	if dep := labels["deployment"]; dep != "" && ns != "" {
		targets = append(targets, contracts.TargetRef{
			Kind:        "k8s.deployment",
			Namespace:   ns,
			Name:        dep,
			Cluster:     cluster,
			Environment: env,
		})
	}

	if pod := labels["pod"]; pod != "" && ns != "" {
		targets = append(targets, contracts.TargetRef{
			Kind:        "k8s.pod",
			Namespace:   ns,
			Name:        pod,
			Cluster:     cluster,
			Environment: env,
		})
	}

	return targets
}

func (r *Resolver) resolveTimeWindow(alert *contracts.NormalizedAlert) (time.Time, time.Time) {
	from := alert.StartsAt.Add(-30 * time.Minute)

	to := time.Now().UTC()
	if alert.EndsAt != nil {
		to = *alert.EndsAt
	}

	return from, to
}

func firstLabel(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := labels[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func dedup(refs []contracts.TargetRef) []contracts.TargetRef {
	seen := make(map[string]struct{}, len(refs))
	out := make([]contracts.TargetRef, 0, len(refs))
	for _, ref := range refs {
		key := ref.Kind + "/" + ref.Namespace + "/" + ref.Name + "/" + ref.Cluster
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}
