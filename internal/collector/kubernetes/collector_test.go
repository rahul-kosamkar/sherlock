package kubernetes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestCollector(client *fake.Clientset) *Collector {
	return &Collector{client: client, log: zap.NewNop()}
}

func TestCollector_Name(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	if got := c.Name(); got != "kubernetes" {
		t.Fatalf("Name() = %q, want %q", got, "kubernetes")
	}
}

func TestIsK8sTarget_Deployment(t *testing.T) {
	if !isK8sTarget(contracts.TargetRef{Kind: "k8s.deployment"}) {
		t.Fatal("expected k8s.deployment to be a K8s target")
	}
}

func TestIsK8sTarget_Pod(t *testing.T) {
	if !isK8sTarget(contracts.TargetRef{Kind: "k8s.pod"}) {
		t.Fatal("expected k8s.pod to be a K8s target")
	}
}

func TestIsK8sTarget_Service(t *testing.T) {
	if !isK8sTarget(contracts.TargetRef{Kind: "service"}) {
		t.Fatal("expected service to be a K8s target")
	}
}

func TestIsK8sTarget_Unknown(t *testing.T) {
	if isK8sTarget(contracts.TargetRef{Kind: "database"}) {
		t.Fatal("expected database to not be a K8s target")
	}
}

func TestBuildLabelSelector_Pod(t *testing.T) {
	got := buildLabelSelector(contracts.TargetRef{Kind: "k8s.pod", Name: "web-abc"})
	if got != "" {
		t.Fatalf("buildLabelSelector(k8s.pod) = %q, want empty string", got)
	}
}

func TestBuildLabelSelector_Deployment(t *testing.T) {
	got := buildLabelSelector(contracts.TargetRef{Kind: "k8s.deployment", Name: "web"})
	if got != "app=web" {
		t.Fatalf("buildLabelSelector(k8s.deployment) = %q, want %q", got, "app=web")
	}
}

func TestBuildLabelSelector_Service(t *testing.T) {
	got := buildLabelSelector(contracts.TargetRef{Kind: "service", Name: "api"})
	if got != "app=api" {
		t.Fatalf("buildLabelSelector(service) = %q, want %q", got, "app=api")
	}
}

func TestCollect_SkipsNonK8sTargets(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := contracts.CollectRequest{
		InvestigationID: "inv-1",
		Targets: []contracts.TargetRef{
			{Kind: "database", Namespace: "default", Name: "mydb"},
		},
		TimeFrom: time.Now().Add(-1 * time.Hour),
		TimeTo:   time.Now(),
	}

	ev, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence, got %d", len(ev))
	}
}

func baseRequest() contracts.CollectRequest {
	return contracts.CollectRequest{
		InvestigationID: "inv-test",
		Targets: []contracts.TargetRef{
			{Kind: "k8s.deployment", Namespace: "default", Name: "web"},
		},
		TimeFrom: time.Now().Add(-1 * time.Hour),
		TimeTo:   time.Now(),
	}
}

func TestPodStateEvidence_CrashLoopBackOff(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-abc", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 5,
					Ready:        false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}

	ev := c.podStateEvidence(req, req.Targets[0], pod)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].Score != scoreCrashLoop {
		t.Fatalf("Score = %f, want %f", ev[0].Score, scoreCrashLoop)
	}
	if !strings.Contains(ev[0].Summary, "CrashLoopBackOff") {
		t.Fatalf("Summary %q should contain CrashLoopBackOff", ev[0].Summary)
	}
}

func TestPodStateEvidence_OOMKilled_LastTermination(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-xyz", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 3,
					Ready:        true,
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"},
					},
				},
			},
		},
	}

	ev := c.podStateEvidence(req, req.Targets[0], pod)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].Score != scoreOOMKilled {
		t.Fatalf("Score = %f, want %f", ev[0].Score, scoreOOMKilled)
	}
}

func TestPodStateEvidence_OOMKilled_Current(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-oom", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 0,
					Ready:        false,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"},
					},
				},
			},
		},
	}

	ev := c.podStateEvidence(req, req.Targets[0], pod)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].Score != scoreOOMKilled {
		t.Fatalf("Score = %f, want %f", ev[0].Score, scoreOOMKilled)
	}
}

func TestPodStateEvidence_Restarts(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-rst", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 2,
					Ready:        true,
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}

	ev := c.podStateEvidence(req, req.Targets[0], pod)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].Score != scorePodNotReady {
		t.Fatalf("Score = %f, want %f", ev[0].Score, scorePodNotReady)
	}
	if !strings.Contains(ev[0].Summary, "restarted") {
		t.Fatalf("Summary %q should mention restarts", ev[0].Summary)
	}
}

func TestPodStateEvidence_NotReady(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-nr", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 0,
					Ready:        false,
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}

	ev := c.podStateEvidence(req, req.Targets[0], pod)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].Score != scorePodNotReady {
		t.Fatalf("Score = %f, want %f", ev[0].Score, scorePodNotReady)
	}
}

func TestPodStateEvidence_Healthy(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-ok", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 0,
					Ready:        true,
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}

	ev := c.podStateEvidence(req, req.Targets[0], pod)
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence for healthy pod, got %d", len(ev))
	}
}

func TestEventEvidence_Warning(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	target := req.Targets[0]
	events := []corev1.Event{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "evt-1", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod", Name: "web-abc",
			},
			Reason:         "BackOff",
			Message:        "Back-off restarting failed container",
			Type:           "Warning",
			Count:          3,
			FirstTimestamp: metav1.Time{Time: req.TimeFrom.Add(5 * time.Minute)},
			LastTimestamp:  metav1.Time{Time: req.TimeFrom.Add(10 * time.Minute)},
		},
	}

	ev := c.eventEvidence(req, target, events)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].Score != scoreWarning {
		t.Fatalf("Score = %f, want %f", ev[0].Score, scoreWarning)
	}
}

func TestEventEvidence_Normal(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	target := req.Targets[0]
	events := []corev1.Event{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "evt-2", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod", Name: "web-abc",
			},
			Reason:         "Pulled",
			Message:        "Successfully pulled image",
			Type:           "Normal",
			Count:          1,
			FirstTimestamp: metav1.Time{Time: req.TimeFrom.Add(5 * time.Minute)},
			LastTimestamp:  metav1.Time{Time: req.TimeFrom.Add(5 * time.Minute)},
		},
	}

	ev := c.eventEvidence(req, target, events)
	if len(ev) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(ev))
	}
	if ev[0].Score != scoreInfoEvent {
		t.Fatalf("Score = %f, want %f", ev[0].Score, scoreInfoEvent)
	}
}

func TestEventEvidence_BeforeTimeWindow(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	target := req.Targets[0]
	events := []corev1.Event{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "evt-old", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod", Name: "web-abc",
			},
			Reason:         "Scheduled",
			Message:        "Successfully assigned",
			Type:           "Normal",
			Count:          1,
			FirstTimestamp: metav1.Time{Time: req.TimeFrom.Add(-2 * time.Hour)},
			LastTimestamp:  metav1.Time{Time: req.TimeFrom.Add(-1 * time.Hour)},
		},
	}

	ev := c.eventEvidence(req, target, events)
	if len(ev) != 0 {
		t.Fatalf("expected 0 evidence for old event, got %d", len(ev))
	}
}

func TestCollectForTarget_WithFakeClient(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-abc123",
			Namespace: "default",
			Labels:    map[string]string{"app": "web"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					RestartCount: 3,
					Ready:        false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "evt-1", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: "web-abc123", Namespace: "default",
		},
		Reason:         "BackOff",
		Message:        "Back-off restarting failed container",
		Type:           "Warning",
		Count:          5,
		FirstTimestamp: metav1.Time{Time: time.Now().Add(-30 * time.Minute)},
		LastTimestamp:  metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
	}

	fakeClient := fake.NewSimpleClientset(pod, event)
	c := newTestCollector(fakeClient)

	req := baseRequest()
	evidence, err := c.collectForTarget(context.Background(), req, req.Targets[0])
	if err != nil {
		t.Fatalf("collectForTarget() error = %v", err)
	}

	if len(evidence) == 0 {
		t.Fatal("expected evidence from collectForTarget, got 0")
	}

	var hasCrashLoop, hasEvent bool
	for _, e := range evidence {
		if e.Kind == contracts.EvidenceK8sState && strings.Contains(e.Summary, "CrashLoopBackOff") {
			hasCrashLoop = true
		}
		if e.Kind == contracts.EvidenceEvent {
			hasEvent = true
		}
	}

	if !hasCrashLoop {
		t.Error("expected CrashLoopBackOff evidence from pod state")
	}
	if !hasEvent {
		t.Error("expected event evidence")
	}
}

func TestCollect_WithFakeClient_IntegrationFlow(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-xyz789",
			Namespace: "default",
			Labels:    map[string]string{"app": "web"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "main",
					RestartCount: 0,
					Ready:        false,
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}

	fakeClient := fake.NewSimpleClientset(pod)
	c := newTestCollector(fakeClient)

	req := contracts.CollectRequest{
		InvestigationID: "inv-full-test",
		Targets: []contracts.TargetRef{
			{Kind: "k8s.deployment", Namespace: "default", Name: "web"},
			{Kind: "database", Namespace: "default", Name: "pg"},
		},
		TimeFrom: time.Now().Add(-1 * time.Hour),
		TimeTo:   time.Now(),
	}

	evidence, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, e := range evidence {
		if e.InvestigationID != "inv-full-test" {
			t.Errorf("evidence has wrong InvestigationID: %s", e.InvestigationID)
		}
	}
}

func TestListPods_WithFakeClient(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-abc",
			Namespace: "prod",
			Labels:    map[string]string{"app": "api"},
		},
	}
	fakeClient := fake.NewSimpleClientset(pod)
	c := newTestCollector(fakeClient)

	target := contracts.TargetRef{Kind: "k8s.deployment", Namespace: "prod", Name: "api"}
	pods, err := c.listPods(context.Background(), target)
	if err != nil {
		t.Fatalf("listPods() error = %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods))
	}
	if pods[0].Name != "api-abc" {
		t.Errorf("pod Name = %q, want %q", pods[0].Name, "api-abc")
	}
}

func TestListEvents_WithFakeClient(t *testing.T) {
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "test-evt", Namespace: "prod"},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: "api-abc", Namespace: "prod",
		},
		Reason:  "Killing",
		Message: "Stopping container",
		Type:    "Normal",
		Count:   1,
	}
	fakeClient := fake.NewSimpleClientset(event)
	c := newTestCollector(fakeClient)

	target := contracts.TargetRef{Kind: "k8s.deployment", Namespace: "prod", Name: "api"}
	events, err := c.listEvents(context.Background(), target)
	if err != nil {
		t.Fatalf("listEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestPreviousLogEvidence_NoRestarts(t *testing.T) {
	c := newTestCollector(fake.NewSimpleClientset())
	req := baseRequest()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-clean", Namespace: "default"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", RestartCount: 0, Ready: true},
			},
		},
	}
	ev := c.previousLogEvidence(context.Background(), req, req.Targets[0], pod)
	if len(ev) != 0 {
		t.Fatalf("expected 0 log evidence for pod with no restarts, got %d", len(ev))
	}
}

func TestBuildLabelSelector_Default(t *testing.T) {
	got := buildLabelSelector(contracts.TargetRef{Kind: "unknown-kind", Name: "myapp"})
	if got != "app=myapp" {
		t.Fatalf("buildLabelSelector(unknown) = %q, want %q", got, "app=myapp")
	}
}

