package policy

import (
	"strings"
	"testing"
)

func TestDefaultRecommendation(t *testing.T) {
	recommendation := Recommend(TaskProfile{CreditState: CreditUnknown})
	if recommendation.Model != "GPT-5.6 Luna" || recommendation.ReasoningLevel != "Medium" || recommendation.ModelRole != "fast/default" {
		t.Fatalf("unexpected default recommendation: %+v", recommendation)
	}
	if recommendation.CreditState != CreditUnknown {
		t.Fatalf("unknown credit state was changed: %+v", recommendation)
	}
}

func TestTaskClassificationUsesConservativeReadOnlyDefault(t *testing.T) {
	cases := []struct {
		name string
		text string
		want TaskIntent
	}{
		{"read only", "Establish current state and report status", IntentReadOnly},
		{"explicit override", "Fix the issue, but do not modify files", IntentReadOnly},
		{"implementation", "Implement the guard fix", IntentImplementation},
		{"review", "Audit the registry behavior", IntentReview},
		{"investigation", "Determine cause of the timeout", IntentInvestigation},
		{"ambiguous", "Tell me what is happening", IntentReadOnly},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyTask(tc.text); got != tc.want {
				t.Fatalf("ClassifyTask(%q) = %s, want %s", tc.text, got, tc.want)
			}
		})
	}
}

func TestRiskEscalatesAndCreditPressureDoesNotBreakSafety(t *testing.T) {
	constrained := Recommend(TaskProfile{SecuritySensitive: true, CreditState: CreditCritical})
	if constrained.Model != "GPT-5.6 Sol" || constrained.ReasoningLevel != "High" {
		t.Fatalf("security task was downgraded unsafely: %+v", constrained)
	}
	cheap := Recommend(TaskProfile{ArchitectureComplex: true, CreditState: CreditConstrained})
	if cheap.Model != "GPT-5.6 Luna" || cheap.ReasoningLevel != "Medium" {
		t.Fatalf("safe constrained task did not conserve budget: %+v", cheap)
	}
}

func TestPromptIsConciseAndCarriesRepositoryContract(t *testing.T) {
	prompt := BuildExecutionPrompt(PromptInput{Goal: "Fix the guard regression.", Context: []string{"AGENTS.md", "docs/agent/START-HERE.md"}, Verification: []string{"go test ./..."}})
	for _, expected := range []string{"Goal", "AGENTS.md", "source, tests, current Git state", "Preserve unrelated dirty work", "Verification", "go test ./..."} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestReadOnlyPromptDoesNotUseImplementationContract(t *testing.T) {
	prompt := BuildExecutionPrompt(PromptInput{Intent: IntentReadOnly, Goal: "Establish current state", Verification: []string{"go test ./..."}, AcceptedStateUnverified: true})
	if strings.Contains(prompt, "Implement the requested change") || strings.Contains(prompt, "go test ./...") || strings.Contains(prompt, "Make the smallest maintainable change") {
		t.Fatalf("read-only prompt forced implementation verification: %s", prompt)
	}
	for _, expected := range []string{"Report only the requested repository-grounded findings.", "Do not modify files.", "Treat current-state documentation as a lead, not verified truth."} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("read-only prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestTaskAwareRecommendationReasons(t *testing.T) {
	for _, tc := range []struct {
		intent TaskIntent
		want   string
	}{
		{IntentReadOnly, "bounded, read-only"},
		{IntentImplementation, "localized"},
		{IntentReview, "evidence review"},
		{IntentInvestigation, "diagnosis"},
	} {
		recommendation := Recommend(TaskProfile{Intent: tc.intent, CreditState: CreditUnknown})
		if !strings.Contains(recommendation.Reason, tc.want) {
			t.Fatalf("intent %s reason %q did not contain %q", tc.intent, recommendation.Reason, tc.want)
		}
	}
}

func TestFutureModelMappingIsSeparateFromRoles(t *testing.T) {
	catalog := CurrentCatalog()
	if catalog.FastDefault == catalog.Strongest || catalog.FastDefault == "" || catalog.Strongest == "" {
		t.Fatalf("model roles are not independently mapped: %+v", catalog)
	}
}
