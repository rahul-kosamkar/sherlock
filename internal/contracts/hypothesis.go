package contracts

type CauseCategory string

const (
	CauseDeploy     CauseCategory = "deploy"
	CauseCapacity   CauseCategory = "capacity"
	CauseDependency CauseCategory = "dependency"
	CauseConfig     CauseCategory = "config"
	CauseCode       CauseCategory = "code"
	CauseInfra      CauseCategory = "infra"
	CauseNoise      CauseCategory = "noise"
)

// Hypothesis represents a ranked explanation for an incident,
// always backed by evidence IDs.
type Hypothesis struct {
	ID            string        `json:"id"`
	Title         string        `json:"title"`
	Narrative     string        `json:"narrative"`
	CauseCategory CauseCategory `json:"cause_category"`
	Confidence    float64       `json:"confidence"`
	Supporting    []string      `json:"supporting"`    // Evidence IDs
	Contradicting []string      `json:"contradicting"` // Evidence IDs
	SuggestedFixes []SuggestedFix `json:"suggested_fixes"`
}

type SuggestedFix struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	RunbookURL    string `json:"runbook_url,omitempty"`
	ActionRef     string `json:"action_ref,omitempty"`
	SafeByDefault bool   `json:"safe_by_default"`
}
