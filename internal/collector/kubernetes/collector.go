package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	sourceName       = "kubernetes"
	prevLogLines     = int64(100)
	scoreOOMKilled   = 0.9
	scoreCrashLoop   = 0.85
	scoreWarning     = 0.6
	scoreInfoEvent   = 0.3
	scorePodNotReady = 0.5
)

type Collector struct {
	client kubernetes.Interface
	log    *zap.Logger
}

func New(kubeconfig string, inCluster bool, log *zap.Logger) (*Collector, error) {
	var cfg *rest.Config
	var err error

	if inCluster {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return nil, fmt.Errorf("kubernetes config: %w", err)
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes clientset: %w", err)
	}

	return &Collector{client: cs, log: log}, nil
}

func (c *Collector) Name() string { return sourceName }

func (c *Collector) Collect(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	var evidence []contracts.Evidence

	for _, target := range req.Targets {
		if !isK8sTarget(target) {
			continue
		}

		ev, err := c.collectForTarget(ctx, req, target)
		if err != nil {
			c.log.Warn("failed to collect for target",
				zap.String("target", target.Kind+"/"+target.Namespace+"/"+target.Name),
				zap.Error(err),
			)
			continue
		}
		evidence = append(evidence, ev...)
	}

	return evidence, nil
}

func (c *Collector) collectForTarget(ctx context.Context, req contracts.CollectRequest, target contracts.TargetRef) ([]contracts.Evidence, error) {
	var evidence []contracts.Evidence

	pods, err := c.listPods(ctx, target)
	if err != nil {
		return nil, err
	}

	for i := range pods {
		pod := &pods[i]
		evidence = append(evidence, c.podStateEvidence(req, target, pod)...)
		evidence = append(evidence, c.previousLogEvidence(ctx, req, target, pod)...)
	}

	events, err := c.listEvents(ctx, target)
	if err != nil {
		c.log.Warn("failed to list events", zap.Error(err))
	} else {
		evidence = append(evidence, c.eventEvidence(req, target, events)...)
	}

	return evidence, nil
}

func (c *Collector) listPods(ctx context.Context, target contracts.TargetRef) ([]corev1.Pod, error) {
	labelSelector := buildLabelSelector(target)

	list, err := c.client.CoreV1().Pods(target.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	return list.Items, nil
}

func (c *Collector) listEvents(ctx context.Context, target contracts.TargetRef) ([]corev1.Event, error) {
	fieldSelector := fmt.Sprintf("involvedObject.namespace=%s", target.Namespace)
	if target.Kind == "k8s.pod" {
		fieldSelector += fmt.Sprintf(",involvedObject.name=%s", target.Name)
	}

	list, err := c.client.CoreV1().Events(target.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return list.Items, nil
}

func (c *Collector) podStateEvidence(req contracts.CollectRequest, target contracts.TargetRef, pod *corev1.Pod) []contracts.Evidence {
	var evidence []contracts.Evidence
	now := time.Now().UTC()

	for _, cs := range pod.Status.ContainerStatuses {
		var summary string
		var score float64

		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff":
			summary = fmt.Sprintf("Container %s in CrashLoopBackOff (restarts: %d)", cs.Name, cs.RestartCount)
			score = scoreCrashLoop
		case cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled":
			summary = fmt.Sprintf("Container %s OOMKilled (restarts: %d)", cs.Name, cs.RestartCount)
			score = scoreOOMKilled
		case cs.State.Terminated != nil && cs.State.Terminated.Reason == "OOMKilled":
			summary = fmt.Sprintf("Container %s currently OOMKilled", cs.Name)
			score = scoreOOMKilled
		case cs.RestartCount > 0:
			summary = fmt.Sprintf("Container %s has restarted %d times", cs.Name, cs.RestartCount)
			score = scorePodNotReady
		case !cs.Ready:
			summary = fmt.Sprintf("Container %s not ready", cs.Name)
			score = scorePodNotReady
		default:
			continue
		}

		evidence = append(evidence, contracts.Evidence{
			ID:              uuid.NewString(),
			InvestigationID: req.InvestigationID,
			Kind:            contracts.EvidenceK8sState,
			Source:          sourceName,
			Target:          target,
			CollectedAt:     now,
			ObservedAtFrom:  req.TimeFrom,
			ObservedAtTo:    req.TimeTo,
			Summary:         fmt.Sprintf("[pod/%s] %s", pod.Name, summary),
			Score:           score,
			Attributes: map[string]string{
				"pod":            pod.Name,
				"container":      cs.Name,
				"restart_count":  fmt.Sprintf("%d", cs.RestartCount),
				"phase":          string(pod.Status.Phase),
			},
			RedactionState: contracts.RedactionNone,
		})
	}

	return evidence
}

func (c *Collector) previousLogEvidence(ctx context.Context, req contracts.CollectRequest, target contracts.TargetRef, pod *corev1.Pod) []contracts.Evidence {
	var evidence []contracts.Evidence
	now := time.Now().UTC()

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.RestartCount == 0 {
			continue
		}

		lines := prevLogLines
		logs, err := c.client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Container: cs.Name,
			Previous:  true,
			TailLines: &lines,
		}).Do(ctx).Raw()
		if err != nil {
			c.log.Debug("failed to get previous logs",
				zap.String("pod", pod.Name),
				zap.String("container", cs.Name),
				zap.Error(err),
			)
			continue
		}

		if len(logs) == 0 {
			continue
		}

		evidence = append(evidence, contracts.Evidence{
			ID:              uuid.NewString(),
			InvestigationID: req.InvestigationID,
			Kind:            contracts.EvidenceLog,
			Source:          sourceName,
			Target:          target,
			CollectedAt:     now,
			ObservedAtFrom:  req.TimeFrom,
			ObservedAtTo:    req.TimeTo,
			Summary:         fmt.Sprintf("[pod/%s] Previous container logs for %s (%d restarts)", pod.Name, cs.Name, cs.RestartCount),
			BodyRef:         string(logs),
			Score:           scorePodNotReady,
			Attributes: map[string]string{
				"pod":       pod.Name,
				"container": cs.Name,
			},
			RedactionState: contracts.RedactionNone,
		})
	}

	return evidence
}

func (c *Collector) eventEvidence(req contracts.CollectRequest, target contracts.TargetRef, events []corev1.Event) []contracts.Evidence {
	var evidence []contracts.Evidence
	now := time.Now().UTC()

	for i := range events {
		ev := &events[i]
		if ev.LastTimestamp.Time.Before(req.TimeFrom) {
			continue
		}

		score := scoreInfoEvent
		if ev.Type == "Warning" {
			score = scoreWarning
		}

		evidence = append(evidence, contracts.Evidence{
			ID:              uuid.NewString(),
			InvestigationID: req.InvestigationID,
			Kind:            contracts.EvidenceEvent,
			Source:          sourceName,
			Target:          target,
			CollectedAt:     now,
			ObservedAtFrom:  ev.FirstTimestamp.Time,
			ObservedAtTo:    ev.LastTimestamp.Time,
			Summary:         fmt.Sprintf("[%s] %s: %s (x%d)", ev.InvolvedObject.Name, ev.Reason, ev.Message, ev.Count),
			Score:           score,
			Attributes: map[string]string{
				"reason":  ev.Reason,
				"type":    ev.Type,
				"object":  ev.InvolvedObject.Kind + "/" + ev.InvolvedObject.Name,
				"count":   fmt.Sprintf("%d", ev.Count),
			},
			RedactionState: contracts.RedactionNone,
		})
	}

	return evidence
}

func isK8sTarget(t contracts.TargetRef) bool {
	switch t.Kind {
	case "k8s.deployment", "k8s.pod", "service":
		return true
	}
	return false
}

func buildLabelSelector(target contracts.TargetRef) string {
	switch target.Kind {
	case "k8s.pod":
		return ""
	case "k8s.deployment":
		return "app=" + target.Name
	case "service":
		return "app=" + target.Name
	default:
		return "app=" + target.Name
	}
}

