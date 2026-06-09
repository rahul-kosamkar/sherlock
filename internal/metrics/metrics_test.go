package metrics

import "testing"

func TestMetricsRegistered(t *testing.T) {
	if WebhooksReceived == nil {
		t.Error("WebhooksReceived is nil")
	}
	if WebhookDuration == nil {
		t.Error("WebhookDuration is nil")
	}
	if InvestigationsStarted == nil {
		t.Error("InvestigationsStarted is nil")
	}
	if InvestigationsCompleted == nil {
		t.Error("InvestigationsCompleted is nil")
	}
	if InvestigationDuration == nil {
		t.Error("InvestigationDuration is nil")
	}
	if LLMCallsTotal == nil {
		t.Error("LLMCallsTotal is nil")
	}
	if LLMCallDuration == nil {
		t.Error("LLMCallDuration is nil")
	}
	if DedupHits == nil {
		t.Error("DedupHits is nil")
	}
	if SuppressHits == nil {
		t.Error("SuppressHits is nil")
	}
	if EvidenceCollected == nil {
		t.Error("EvidenceCollected is nil")
	}
	if ActiveInvestigations == nil {
		t.Error("ActiveInvestigations is nil")
	}
}
