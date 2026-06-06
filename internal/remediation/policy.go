package remediation

type Policy struct {
	Name    string         `yaml:"name"`
	Match   PolicyMatch    `yaml:"match"`
	Actions []PolicyAction `yaml:"actions"`
}

type PolicyMatch struct {
	CauseCategory string  `yaml:"cause_category"`
	ConfidenceMin float64 `yaml:"confidence_min"`
}

type PolicyAction struct {
	Title         string `yaml:"title"`
	Description   string `yaml:"description"`
	RunbookURL    string `yaml:"runbook_url"`
	SafeByDefault bool   `yaml:"safe_by_default"`
}
