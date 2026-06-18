package llm

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

var (
	fieldOrder = []string{
		"SUMMARY", "ROOT_CAUSE", "SEVERITY", "EXIT_TYPE",
		"ACTION_REQUIRED", "BUG_FIXABLE", "CONFIDENCE",
		"RECOMMENDATIONS", "FOLLOW_UP",
	}

	followUpRe = regexp.MustCompile(
		`(?m)^-\s*(TRACE_LOGS|TIME_WINDOW_LOGS|POD_EVENTS|GITHUB_FILES|LOG_QUERY):\s*(.+)$`,
	)
	recommendationRe = regexp.MustCompile(`(?m)^-\s+(.+)$`)
)

func ParseAnalysis(raw string) *LLMAnalysis {
	fields := parseFields(raw)

	analysis := &LLMAnalysis{
		RawResponse: raw,
	}

	analysis.Summary = cleanMultiline(fields["SUMMARY"])
	analysis.RootCause = cleanMultiline(fields["ROOT_CAUSE"])
	analysis.Severity = strings.ToLower(strings.TrimSpace(fields["SEVERITY"]))
	analysis.ExitType = strings.ToLower(strings.TrimSpace(fields["EXIT_TYPE"]))
	analysis.ActionRequired = isYes(fields["ACTION_REQUIRED"])
	analysis.BugFixable = isYes(fields["BUG_FIXABLE"])
	analysis.Confidence = strings.ToLower(strings.TrimSpace(fields["CONFIDENCE"]))

	if recBlock, ok := fields["RECOMMENDATIONS"]; ok {
		matches := recommendationRe.FindAllStringSubmatch(recBlock, -1)
		for _, m := range matches {
			text := strings.TrimSpace(m[1])
			if text != "" && !isFollowUpTool(text) {
				analysis.Recommendations = append(analysis.Recommendations, text)
			}
		}
	}

	analysis.FollowUps = ParseFollowUps(raw)

	return analysis
}

func MapToHypotheses(analysis *LLMAnalysis, evidence []contracts.Evidence) []contracts.Hypothesis {
	supporting := make([]string, 0, len(evidence))
	for _, e := range evidence {
		supporting = append(supporting, e.ID)
	}

	fixes := make([]contracts.SuggestedFix, 0, len(analysis.Recommendations))
	for i, rec := range analysis.Recommendations {
		fixes = append(fixes, contracts.SuggestedFix{
			Title:       fmt.Sprintf("Step %d", i+1),
			Description: rec,
		})
	}

	h := contracts.Hypothesis{
		Title:          analysis.Summary,
		Narrative:      analysis.RootCause,
		CauseCategory:  inferCauseCategory(analysis),
		Confidence:     mapConfidence(analysis.Confidence),
		Supporting:     supporting,
		Contradicting:  nil,
		SuggestedFixes: fixes,
	}

	return []contracts.Hypothesis{h}
}

func ParseFollowUps(raw string) []FollowUpQuery {
	fuIdx := strings.Index(raw, "FOLLOW_UP:")
	if fuIdx < 0 {
		return nil
	}
	fuBlock := raw[fuIdx:]

	matches := followUpRe.FindAllStringSubmatch(fuBlock, -1)
	queries := make([]FollowUpQuery, 0, len(matches))
	for _, m := range matches {
		queries = append(queries, FollowUpQuery{
			Tool:  strings.TrimSpace(m[1]),
			Value: strings.TrimSpace(m[2]),
		})
	}
	return queries
}

func parseFields(raw string) map[string]string {
	fields := make(map[string]string, len(fieldOrder))

	type fieldLoc struct {
		name  string
		start int
		end   int
	}

	var locs []fieldLoc
	for _, name := range fieldOrder {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `:\s*`)
		loc := re.FindStringIndex(raw)
		if loc != nil {
			locs = append(locs, fieldLoc{name: name, start: loc[0], end: loc[1]})
		}
	}

	for i, fl := range locs {
		valueStart := fl.end
		var valueEnd int
		if i+1 < len(locs) {
			valueEnd = locs[i+1].start
		} else {
			valueEnd = len(raw)
		}
		fields[fl.name] = strings.TrimSpace(raw[valueStart:valueEnd])
	}

	return fields
}

func cleanMultiline(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned = append(cleaned, strings.TrimSpace(line))
	}
	return strings.Join(cleaned, " ")
}

func isYes(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "yes")
}

func isFollowUpTool(s string) bool {
	tools := []string{"TRACE_LOGS:", "TIME_WINDOW_LOGS:", "POD_EVENTS:", "GITHUB_FILES:", "LOG_QUERY:"}
	upper := strings.ToUpper(s)
	for _, t := range tools {
		if strings.HasPrefix(upper, t) {
			return true
		}
	}
	return false
}

func inferCauseCategory(analysis *LLMAnalysis) contracts.CauseCategory {
	text := strings.ToLower(analysis.RootCause + " " + analysis.Summary + " " + analysis.ExitType)

	switch {
	case containsAny(text, "deploy", "rollout", "release", "rollback", "version"):
		return contracts.CauseDeploy
	case containsAny(text, "memory", "oom", "heap", "out of memory", "resource limit", "evict"):
		return contracts.CauseCapacity
	case containsAny(text, "infra", "node", "scheduling", "disk", "network partition", "hardware"):
		return contracts.CauseInfra
	case containsAny(text, "config", "environment variable", "misconfigur", "secret", "flag"):
		return contracts.CauseConfig
	case containsAny(text, "dependency", "upstream", "downstream", "timeout", "connection refused", "dns"):
		return contracts.CauseDependency
	case containsAny(text, "noise", "flap", "transient", "false positive", "auto-resolved"):
		return contracts.CauseNoise
	case containsAny(text, "code", "bug", "function", "nil pointer", "panic", "exception", "stack trace"):
		return contracts.CauseCode
	default:
		return contracts.CauseCode
	}
}

func mapConfidence(level string) float64 {
	switch strings.TrimSpace(strings.ToLower(level)) {
	case "high":
		return 0.85
	case "medium":
		return 0.65
	case "low":
		return 0.4
	default:
		return 0.5
	}
}

func containsAny(text string, terms ...string) bool {
	for _, t := range terms {
		if strings.Contains(text, t) {
			return true
		}
	}
	return false
}
