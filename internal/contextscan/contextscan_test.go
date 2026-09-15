package contextscan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGreenfieldPreviewAndApplyCreatesOnlyMinimalScaffold(t *testing.T) {
	root := t.TempDir()
	before := snapshot(t, root)
	proposal, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Changes) != 3 || proposal.Reason != "MINIMAL_GREENFIELD_SCAFFOLD" {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	if after := snapshot(t, root); after != before {
		t.Fatal("preview modified fixture")
	}
	if err := Apply(proposal); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"AGENTS.md", "docs/agent/START-HERE.md", "docs/agent/CURRENT-STATE.md"} {
		if !exists(root, path) {
			t.Fatalf("missing %s", path)
		}
	}
	if exists(root, "docs/agent/plans") || exists(root, ".contextbridge") {
		t.Fatal("greenfield scaffold added unrequested structure")
	}
}

func TestExistingSystemsReceiveThinAgentRoutingWithoutReplacement(t *testing.T) {
	root := t.TempDir()
	original := "# Existing\nKeep this repository's instructions.\n"
	write(t, root, "AGENTS.md", original)
	proposal, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Changes) != 1 || proposal.Changes[0].Path != "AGENTS.md" {
		t.Fatalf("expected additive AGENTS routing: %+v", proposal)
	}
	if !strings.HasPrefix(proposal.Changes[0].Content, original) || !strings.Contains(proposal.Changes[0].Content, "This project uses Context Bridge") {
		t.Fatalf("existing AGENTS content was not preserved: %q", proposal.Changes[0].Content)
	}
	if err := Apply(proposal); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), original) {
		t.Fatal("existing AGENTS content was replaced")
	}
}

func TestMatureRepoGetsRouterButNoDuplicateStateSystem(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/agent/START-HERE.md", "start")
	write(t, root, "docs/agent/CURRENT-STATE.md", "state")
	proposal, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Changes) != 1 || proposal.Changes[0].Path != "AGENTS.md" {
		t.Fatalf("expected only a thin router: %+v", proposal)
	}
	if strings.Contains(proposal.Changes[0].Content, ".workspace") || strings.Contains(proposal.Changes[0].Content, "CURRENT-STATE") {
		t.Fatal("router created a duplicate state-system instruction")
	}
}

func TestConflictingAgentInstructionIsNotModified(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "# Existing\nDo not use Context Bridge in this repository.\n")
	proposal, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Changes) != 0 || proposal.Reason != "CONFLICTING_AGENT_INSTRUCTIONS" {
		t.Fatalf("conflicting instructions were not blocked: %+v", proposal)
	}
	report, err := Doctor(root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsRule(report.Findings, "AGENTS_CONFLICT") {
		t.Fatalf("missing AGENTS conflict diagnostic: %+v", report.Findings)
	}
}

func TestCodexReadinessAndMissingRouteDiagnostics(t *testing.T) {
	root := t.TempDir()
	report, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.CodexReady || report.Agents != "missing" {
		t.Fatalf("unexpected greenfield Codex status: %+v", report)
	}
	write(t, root, "AGENTS.md", "# Existing\n")
	report, err = Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.CodexReady || report.Agents != "missing_route" {
		t.Fatalf("unexpected missing-route status: %+v", report)
	}
	doctor, err := Doctor(root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsRule(doctor.Findings, "AGENTS_CONTEXT_BRIDGE_MISSING") {
		t.Fatalf("missing route diagnostic absent: %+v", doctor.Findings)
	}
}

func TestDetectsWorkspaceConflictAndOpenSpec(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/agent/START-HERE.md", "start")
	write(t, root, "docs/agent/CURRENT-STATE.md", "state")
	write(t, root, ".workspace/state.md", "state")
	write(t, root, "openspec/README.md", "spec")
	report, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "conflict" || !containsSystem(report.Systems, "OPENSPEC") {
		t.Fatalf("unexpected inspection: %+v", report)
	}
}

func TestDoctorDetectsBrokenReferenceAndDuplicateState(t *testing.T) {
	root := t.TempDir()
	write(t, root, "AGENTS.md", "Read `docs/agent/MISSING.md`. Run tests.\n")
	write(t, root, "docs/agent/START-HERE.md", "start")
	write(t, root, "docs/agent/CURRENT-STATE.md", "state")
	write(t, root, ".workspace/current.md", "state")
	report, err := Doctor(root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsRule(report.Findings, "BROKEN_REFERENCE:docs/agent/MISSING.md") || !containsRulePrefix(report.Findings, "DUPLICATE_CONTEXT_SYSTEM") {
		t.Fatalf("missing expected findings: %+v", report.Findings)
	}
}

func TestDoctorDetectsWindowsPathAndSecretWithoutReportingValue(t *testing.T) {
	root := t.TempDir()
	secret := "not-a-real-secret-value"
	write(t, root, "AGENTS.md", "Use C:\\Users\\example\\work. token="+secret+". Run tests.\n")
	report, err := Doctor(root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsRule(report.Findings, "WINDOWS_USER_PATH") || !containsRule(report.Findings, "SECRET_LIKE_CONTENT") {
		t.Fatalf("missing security rules: %+v", report.Findings)
	}
	for _, finding := range report.Findings {
		if strings.Contains(finding.Rule, secret) || strings.Contains(finding.Path, secret) {
			t.Fatal("secret value leaked into finding")
		}
	}
}

func TestApplyRefusesExistingFileAndDoctorIsReadOnly(t *testing.T) {
	root := t.TempDir()
	proposal, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "AGENTS.md", "user content")
	if err := Apply(proposal); err == nil {
		t.Fatal("apply overwrote existing file")
	}
	before := snapshot(t, root)
	if _, err := Doctor(root); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, root); after != before {
		t.Fatal("doctor modified fixture")
	}
}

func write(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
func snapshot(t *testing.T, root string) string {
	t.Helper()
	entries := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(path, root)), "/")+":"+string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(entries, "|")
}
func containsSystem(systems []System, name string) bool {
	for _, system := range systems {
		if system.Name == name {
			return true
		}
	}
	return false
}
func containsRule(findings []Finding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}
func containsRulePrefix(findings []Finding, prefix string) bool {
	for _, finding := range findings {
		if strings.HasPrefix(finding.Rule, prefix) {
			return true
		}
	}
	return false
}
