package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/codexbootstrap"
)

func TestInstructionsCodexReportsChainLimitsAndRedactsContents(t *testing.T) {
	fixture := newHandoffFixture(t)
	nested := filepath.Join(fixture.repository, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := "super-secret-token-value"
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("project instruction\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.override.md"), []byte(strings.Repeat("override "+secret+"\n", 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "AGENTS.md"), []byte("global instruction\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("project_doc_max_bytes = 100\nsandbox_mode = \"workspace-write\"\napproval_policy = \"on-request\"\nwritable_roots = [\"C:/allowed\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	t.Setenv("CODEX_HOME", codexHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"instructions", "--explain", "--project", nested, "--agent", "codex", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("instructions failed: %s", stderr.String())
	}
	var result instructionResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != readinessWarned || result.Diagnostics.Codex == nil || result.Diagnostics.Codex.PredictedTruncationRisk != "EXCEEDS_LIMIT" {
		t.Fatalf("unexpected codex diagnostics: %#v", result)
	}
	if len(result.Diagnostics.InstructionChain) != 3 || !hasDiagnosticReason(result.Diagnostics.Warnings, "INSTRUCTION_OVERRIDE_PRESENT") {
		t.Fatalf("instruction chain or override warning missing: %#v", result.Diagnostics)
	}
	expectedPaths := []string{
		canonicalTestPathKey(t, filepath.Join(codexHome, "AGENTS.md")),
		canonicalTestPathKey(t, filepath.Join(fixture.repository, "AGENTS.md")),
		canonicalTestPathKey(t, filepath.Join(nested, "AGENTS.override.md")),
	}
	for index, expected := range expectedPaths {
		if !equivalentTestPath(t, result.Diagnostics.InstructionChain[index].Path, expected) {
			t.Fatalf("Codex precedence order was incorrect: %#v", result.Diagnostics.InstructionChain)
		}
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stdout.String(), "token-value") {
		t.Fatalf("instruction contents leaked into diagnostics: %s", stdout.String())
	}
	for _, source := range result.Diagnostics.InstructionChain {
		if equivalentTestPath(t, source.Path, filepath.Join(nested, "AGENTS.md")) && !source.InsideRepository {
			t.Fatal("nested project instruction was marked outside the repository")
		}
	}
}

func TestInstructionsClaudeAndCursorDiagnostics(t *testing.T) {
	fixture := newHandoffFixture(t)
	claudeConfig := t.TempDir()
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeConfig)
	if err := os.WriteFile(filepath.Join(fixture.repository, "CLAUDE.md"), []byte("@AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.repository, "CLAUDE.local.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fixture.repository, ".claude", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.repository, ".claude", "rules", "paths.md"), []byte("rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeConfig, "CLAUDE.md"), []byte("global\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"instructions", "--explain", "--project", fixture.repository, "--agent", "claude", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("claude diagnostics failed: %s", stderr.String())
	}
	var claude instructionResult
	if err := json.Unmarshal(stdout.Bytes(), &claude); err != nil {
		t.Fatal(err)
	}
	if claude.Diagnostics.Claude == nil || !claude.Diagnostics.Claude.CLAUDELocalPresent || !claude.Diagnostics.Claude.AGENTSImportPresent || len(claude.Diagnostics.Claude.RulesPaths) != 1 {
		t.Fatalf("unexpected Claude diagnostics: %#v", claude.Diagnostics.Claude)
	}
	if err := os.MkdirAll(filepath.Join(fixture.repository, ".cursor", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.repository, ".cursor", "rules", "rule.mdc"), []byte("cursor rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"instructions", "--explain", "--project", fixture.repository, "--agent", "cursor", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("cursor diagnostics failed: %s", stderr.String())
	}
	var cursor instructionResult
	if err := json.Unmarshal(stdout.Bytes(), &cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.Diagnostics.Cursor == nil || len(cursor.Diagnostics.Cursor.RulePaths) == 0 {
		t.Fatalf("unexpected Cursor diagnostics: %#v", cursor.Diagnostics.Cursor)
	}
}

func TestInstructionsReportsAbsentClaudeAndCursorConfiguration(t *testing.T) {
	fixture := newHandoffFixture(t)
	claudeConfig := filepath.Join(t.TempDir(), "missing-claude")
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeConfig)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"instructions", "--explain", "--project", fixture.repository, "--agent", "claude"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("claude diagnostics failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Claude instructions: none detected") || !strings.Contains(stdout.String(), "Claude local config: not detected") {
		t.Fatalf("absent Claude configuration was not explicit: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"instructions", "--explain", "--project", fixture.repository, "--agent", "cursor"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("cursor diagnostics failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Cursor rules: none detected") || !strings.Contains(stdout.String(), "Cursor worktree config: not detected") {
		t.Fatalf("absent Cursor configuration was not explicit: %s", stdout.String())
	}
}

func TestDiagnosticWarningsCarrySeverityAndAction(t *testing.T) {
	issue := warningFor("GLOBAL_INSTRUCTION_PRESENT")
	if issue.Severity != "info" || issue.Message == "" || issue.Action == "" {
		t.Fatalf("warning metadata incomplete: %#v", issue)
	}
	issue = warningFor("AGENT_CONFIG_UNVERIFIED")
	if issue.Severity != "warning" || issue.Message == "" || issue.Action == "" {
		t.Fatalf("configuration warning metadata incomplete: %#v", issue)
	}
}

func TestDiagnosticWarningsAreDeduplicated(t *testing.T) {
	diagnostics := agentDiagnostics{InstructionChain: []instructionSource{{Scope: "global"}, {Scope: "global"}}, Warnings: []diagnosticIssue{}}
	deriveDiagnosticWarnings(&diagnostics)
	if len(diagnostics.Warnings) != 1 || diagnostics.Warnings[0].Reason != "GLOBAL_INSTRUCTION_PRESENT" {
		t.Fatalf("duplicate warning was emitted: %#v", diagnostics.Warnings)
	}
}

func TestInstructionsRejectsUnknownAgent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"instructions", "--explain", "--agent", "unknown"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "agent must be codex") {
		t.Fatalf("unknown agent was not rejected: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCodexDiagnosticsReportProfileRouterAndSafeAction(t *testing.T) {
	fixture := newHandoffFixture(t)
	codexHome := filepath.Join(t.TempDir(), "codex")
	t.Setenv("CONTEXTBRIDGE_HOME", filepath.Join(t.TempDir(), "contextbridge"))
	t.Setenv("CODEX_HOME", codexHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"instructions", "--explain", "--project", fixture.repository, "--agent", "codex", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("diagnostics failed: %s", stderr.String())
	}
	var result instructionResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Diagnostics.WorkspaceProfile != "NOT_CONFIGURED" || result.Diagnostics.Codex == nil || result.Diagnostics.Codex.GlobalRouter.Status != codexbootstrap.StatusMissing {
		t.Fatalf("profile or router status missing: router=%#v diagnostics=%#v", result.Diagnostics.Codex.GlobalRouter, result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics.SafeNextAction, "contextbridge setup --workspace-root") || !hasDiagnosticReason(result.Diagnostics.Warnings, "CODEX_GLOBAL_ROUTER_MISSING") {
		t.Fatalf("safe action or router warning missing: %#v", result.Diagnostics)
	}
}

func hasDiagnosticReason(issues []diagnosticIssue, expected string) bool {
	for _, issue := range issues {
		if issue.Reason == expected {
			return true
		}
	}
	return false
}
