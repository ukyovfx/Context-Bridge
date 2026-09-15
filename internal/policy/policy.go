// Package policy contains deterministic, provider-neutral execution prompt and
// model recommendation policy. It does not call models or persist state.
package policy

import (
	"fmt"
	"strings"
)

type CreditState string

type TaskIntent string

const (
	IntentReadOnly       TaskIntent = "read_only"
	IntentImplementation TaskIntent = "implementation"
	IntentReview         TaskIntent = "review"
	IntentInvestigation  TaskIntent = "investigation"
)

func ClassifyTask(task string) TaskIntent {
	lower := strings.ToLower(strings.TrimSpace(task))
	if containsAny(lower, "do not modify", "don't modify", "without modifying", "read-only", "read only", "report only", "establish current state", "report status", "summarize", "inspect", "explain", "find", "compare without modifying") {
		return IntentReadOnly
	}
	if containsAny(lower, "review", "audit", "assess", "critique") {
		return IntentReview
	}
	if containsAny(lower, "diagnose", "investigate", "determine cause", "reproduce") {
		return IntentInvestigation
	}
	if containsAny(lower, "implement", "fix", "add", "update", "modify", "refactor", "remove", "migrate") {
		return IntentImplementation
	}
	return IntentReadOnly
}

func containsAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

const (
	CreditAbundant    CreditState = "abundant"
	CreditNormal      CreditState = "normal"
	CreditConstrained CreditState = "constrained"
	CreditCritical    CreditState = "critical"
	CreditUnknown     CreditState = "unknown"
)

func ValidCreditState(value CreditState) bool {
	switch value {
	case CreditAbundant, CreditNormal, CreditConstrained, CreditCritical, CreditUnknown:
		return true
	default:
		return false
	}
}

type TaskProfile struct {
	Intent                TaskIntent
	Ambiguous             bool
	ArchitectureComplex   bool
	SecuritySensitive     bool
	DestructiveRisk       bool
	VerificationDifficult bool
	CrossCutting          bool
	OnePassCorrectness    bool
	IncompleteContext     bool
	CreditState           CreditState
}

type Recommendation struct {
	TaskIntent     TaskIntent  `json:"task_intent"`
	ModelRole      string      `json:"model_role"`
	Model          string      `json:"model"`
	ReasoningLevel string      `json:"reasoning_level"`
	Reason         string      `json:"reason"`
	CreditState    CreditState `json:"credit_state"`
	Fallback       string      `json:"fallback,omitempty"`
}

type ModelCatalog struct {
	FastDefault     string
	DeeperReasoning string
	Stronger        string
	Strongest       string
}

// CurrentCatalog is the replaceable provider/model mapping. Policy decisions
// use roles; changing model names does not require changing project state.
func CurrentCatalog() ModelCatalog {
	return ModelCatalog{FastDefault: "GPT-5.6 Luna", DeeperReasoning: "GPT-5.6 Luna", Stronger: "GPT-5.6 Sol", Strongest: "GPT-6 Astra"}
}

func Recommend(profile TaskProfile) Recommendation {
	if !ValidCreditState(profile.CreditState) {
		profile.CreditState = CreditUnknown
	}
	catalog := CurrentCatalog()
	if profile.Intent == "" {
		profile.Intent = IntentImplementation
	}
	recommendation := Recommendation{TaskIntent: profile.Intent, ModelRole: "fast/default", Model: catalog.FastDefault, ReasoningLevel: "Medium", CreditState: profile.CreditState, Fallback: "stronger/deeper reasoning only when task risk or ambiguity requires it"}
	if highRisk(profile) {
		return Recommendation{TaskIntent: profile.Intent, ModelRole: "deeper-reasoning", Model: catalog.Stronger, ReasoningLevel: "High", Reason: "The task has safety or correctness risk and needs careful regression analysis.", CreditState: profile.CreditState, Fallback: "strongest current model only if ambiguity remains after repository inspection"}
	}
	if profile.Intent == IntentReadOnly {
		recommendation.Reason = "The task is bounded, read-only, and directly verifiable from repository evidence."
		return recommendation
	}
	if profile.Intent == IntentReview {
		recommendation.Reason = "The task requires evidence review but no architectural implementation."
		return recommendation
	}
	if profile.Intent == IntentInvestigation {
		recommendation.Reason = "The diagnosis is bounded but requires correlating multiple repository signals."
		return recommendation
	}
	if profile.Ambiguous || profile.ArchitectureComplex || profile.VerificationDifficult || profile.CrossCutting || profile.OnePassCorrectness || profile.IncompleteContext {
		recommendation.ModelRole = "deeper-reasoning"
		recommendation.ReasoningLevel = "High"
		recommendation.Reason = "The task needs additional reasoning to resolve complexity or verification uncertainty."
		if profile.CreditState == CreditConstrained || profile.CreditState == CreditCritical {
			recommendation.ReasoningLevel = "Medium"
			recommendation.Reason = "The task is non-critical, so constrained budget favors the reliable default setting."
		}
		return recommendation
	}
	recommendation.Reason = "The change is localized and strongly verifiable with existing repository checks."
	if profile.CreditState == CreditAbundant {
		recommendation.Reason = "Budget is available, but the localized task does not justify escalation beyond the reliable default."
	}
	return recommendation
}

func highRisk(profile TaskProfile) bool { return profile.SecuritySensitive || profile.DestructiveRisk }

type PromptInput struct {
	Intent                  TaskIntent
	Goal                    string
	Context                 []string
	Constraints             []string
	DoneConditions          []string
	Verification            []string
	AcceptedStateUnverified bool
}

func BuildExecutionPrompt(input PromptInput) string {
	var sections []string
	if goal := strings.TrimSpace(input.Goal); goal != "" {
		sections = append(sections, "Goal\n\n"+goal)
	}
	if len(input.Context) > 0 {
		sections = append(sections, "Context\n\n"+bullets(input.Context))
	}
	constraints := append([]string{}, input.Constraints...)
	constraints = append(constraints,
		"Read applicable AGENTS.md and repository instructions first.",
		"Treat source, tests, current Git state, and verified repository documentation as authoritative.",
		"Preserve unrelated dirty work and follow existing repository patterns.",
		"Do not ask questions answerable from repository files, tests, logs, or tools.",
	)
	if input.Intent == IntentReadOnly {
		constraints = append(constraints, "Read only the minimum context necessary and inspect relevant evidence before reporting claims.")
	} else {
		constraints = append(constraints, "Read only the minimum context necessary and inspect relevant implementation before making claims or edits.")
	}
	if input.Intent != IntentReadOnly {
		constraints = append(constraints, "Make the smallest maintainable change; avoid unrelated refactors, cleanup, dependency upgrades, and speculative improvements.")
	}
	if input.Intent == IntentReadOnly {
		constraints = append(constraints, "Do not modify files.")
	} else if input.Intent == IntentReview {
		constraints = append(constraints, "Do not edit files unless the task explicitly requests an edit.")
	} else if input.Intent == IntentInvestigation {
		constraints = append(constraints, "Keep changes disabled unless reproduction or instrumentation is explicitly authorized.")
	}
	if input.AcceptedStateUnverified && input.Intent == IntentReadOnly {
		constraints = append(constraints, "Treat current-state documentation as a lead, not verified truth. Cross-check it against current Git state, source, tests, CI, and other higher-authority repository evidence before reporting current state.")
	}
	sections = append(sections, "Constraints\n\n"+bullets(constraints))
	done := append([]string{}, input.DoneConditions...)
	if input.Intent == IntentReadOnly {
		done = append(done, "Report only the requested repository-grounded findings.", "Do not modify files.")
	} else if input.Intent == IntentReview {
		done = append(done, "Report evidence-backed findings and any risks without making unrequested edits.")
	} else if input.Intent == IntentInvestigation {
		done = append(done, "Report the diagnosis, evidence, and next action without changing files unless explicitly authorized.")
	}
	if len(done) > 0 {
		sections = append(sections, "Done Conditions\n\n"+bullets(done))
	}
	verification := []string{}
	if input.Intent == IntentReadOnly {
		verification = append(verification, "Corroborate claims with current repository evidence; run only commands necessary to establish the requested state.")
	} else {
		verification = append(verification, input.Verification...)
		verification = append(verification, "Run relevant verification before reporting completion.")
	}
	sections = append(sections, "Verification\n\n"+bullets(verification))
	return strings.Join(sections, "\n\n")
}

func bullets(values []string) string {
	clean := uniqueNonEmpty(values)
	lines := make([]string, 0, len(clean))
	for _, value := range clean {
		lines = append(lines, "- "+value)
	}
	return strings.Join(lines, "\n")
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func FormatRecommendation(recommendation Recommendation) string {
	return fmt.Sprintf("Model recommendation: %s\nReasoning level: %s\nReason: %s", recommendation.Model, recommendation.ReasoningLevel, recommendation.Reason)
}
