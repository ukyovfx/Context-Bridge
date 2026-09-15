package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func TestHandoffResolvesAliasRunsGuardAndDoesNotMutate(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	before := directorySnapshot(t, fixture.repository)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"handoff", "pilot", "--task", "Fix notification card layout", "--agent", "codex", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("handoff failed: %s\n%s", stderr.String(), stdout.String())
	}
	var result handoffResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("handoff JSON was invalid: %v\n%s", err, stdout.String())
	}
	if (result.Status != handoffReady && result.Status != handoffReadyWithIssues) || result.Handoff == nil {
		t.Fatalf("unexpected handoff result: %#v", result)
	}
	if result.Handoff.ProjectID != fixture.project.ID || result.Handoff.RepositoryID != fixture.repositoryRecord.ID || result.Handoff.WorkspaceID != fixture.workspace.ID {
		t.Fatalf("handoff identity mismatch: %#v", result.Handoff)
	}
	if result.Handoff.TaskIntent != "Fix notification card layout" || !result.Handoff.Guard.Allowed {
		t.Fatalf("handoff task or guard mismatch: %#v", result.Handoff)
	}
	if result.Handoff.Recommendation.Model != "GPT-5.6 Luna" || result.Handoff.Recommendation.ReasoningLevel != "Medium" {
		t.Fatalf("unexpected default model recommendation: %#v", result.Handoff.Recommendation)
	}
	if !strings.Contains(result.Handoff.ExecutionPrompt, "Read applicable AGENTS.md") || !strings.Contains(result.Handoff.ExecutionPrompt, "Preserve unrelated dirty work") {
		t.Fatalf("execution prompt omitted repository contract: %s", result.Handoff.ExecutionPrompt)
	}
	if result.Handoff.RepositoryIdentity.Path != "ukyovfx/pilot" {
		t.Fatalf("unexpected repository identity: %#v", result.Handoff.RepositoryIdentity)
	}
	if strings.Contains(stdout.String(), "https://") || strings.Contains(stdout.String(), "@") {
		t.Fatalf("handoff leaked raw credential-bearing remote data: %s", stdout.String())
	}
	if after := directorySnapshot(t, fixture.repository); before != after {
		t.Fatalf("handoff mutated the registered repository\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestHandoffCreditStateIsExplicitAndRiskDoesNotDowngrade(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"handoff", fixture.project.ID, "--task", "Review the security authentication migration", "--credit-state", "critical", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("handoff failed: %s", stderr.String())
	}
	var result handoffResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Handoff.Recommendation.CreditState != "critical" || result.Handoff.Recommendation.ReasoningLevel != "High" {
		t.Fatalf("critical risk recommendation was downgraded or invented: %#v", result.Handoff.Recommendation)
	}
	stdout.Reset()
	if code := Run([]string{"handoff", fixture.project.ID, "--task", "Review", "--credit-state", "not-real", "--json"}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("invalid credit state unexpectedly succeeded")
	}
}

func TestHandoffReadOnlyPromptAndRegisteredPathResolution(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	if err := os.WriteFile(filepath.Join(fixture.repository, "docs", "agent", "CURRENT-STATE.md"), []byte("# Current State\nprovenance unavailable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"handoff", fixture.repository, "--task", "Establish current state. Report only status. Do not modify files.", "--credit-state", "normal", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("path handoff failed: %s", stderr.String())
	}
	var result handoffResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Handoff.TaskIntentType != "read_only" {
		t.Fatalf("task was not classified read-only: %#v", result.Handoff)
	}
	if strings.Contains(result.Handoff.ExecutionPrompt, "Implement the requested change") || strings.Contains(result.Handoff.ExecutionPrompt, "go test ./...") {
		t.Fatalf("read-only handoff prompt forced implementation work: %s", result.Handoff.ExecutionPrompt)
	}
	if !strings.Contains(result.Handoff.ExecutionPrompt, "current-state documentation as a lead") {
		t.Fatalf("unverified state warning was omitted: %s", result.Handoff.ExecutionPrompt)
	}
}

func TestHandoffJSONIsDeterministicExceptTimestamp(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	outputs := make([]handoffResult, 2)
	for i := range outputs {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"handoff", fixture.project.ID, "--task", "same task", "--json"}, &stdout, &stderr, "test"); code != 0 {
			t.Fatalf("handoff failed: %s", stderr.String())
		}
		if err := json.Unmarshal(stdout.Bytes(), &outputs[i]); err != nil {
			t.Fatal(err)
		}
		outputs[i].Handoff.GeneratedAt = ""
	}
	left, _ := json.Marshal(outputs[0])
	right, _ := json.Marshal(outputs[1])
	if string(left) != string(right) {
		t.Fatalf("handoff changed beyond generated_at\nleft=%s\nright=%s", left, right)
	}
}

func TestHandoffAllowsBranchChangeAndFailsClosedForMissingProject(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	runTestGit(t, fixture.repository, "checkout", "-b", "legitimate-work")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"handoff", fixture.project.ID, "--task", "inspect current state", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("handoff treated a legitimate branch change as workspace identity drift: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"handoff", "missing", "--task", "must stop", "--json"}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("missing project unexpectedly succeeded")
	}
	if !strings.Contains(stdout.String(), `"PROJECT_NOT_FOUND"`) || strings.Contains(stdout.String(), `"handoff"`) {
		t.Fatalf("missing project result was not minimal and explicit: %s", stdout.String())
	}
}

func TestResolveHandoffProjectReportsAmbiguity(t *testing.T) {
	first := core.ProjectRecord{ID: "prj_11111111-1111-4111-8111-111111111111", DisplayName: "one"}
	second := core.ProjectRecord{ID: "prj_22222222-2222-4222-8222-222222222222", DisplayName: "two", Aliases: []string{"one"}}
	_, err := resolveHandoffProject(core.Registry{Projects: []core.ProjectRecord{first, second}}, "one")
	var resolution handoffResolutionError
	if !errors.As(err, &resolution) || resolution.Reason != handoffReasonProjectAmbiguous || len(resolution.Matches) != 2 {
		t.Fatalf("expected explicit ambiguity, got %v", err)
	}
}

func TestHandoffReportsMissingCanonicalWorkspace(t *testing.T) {
	fixture := newHandoffFixture(t)
	value := core.NewRegistry()
	value.Projects = []core.ProjectRecord{fixture.project}
	value.Repositories = []core.RepositoryRecord{fixture.repositoryRecord}
	if err := (registry.Store{Home: fixture.registryHome}).Save(value); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"handoff", fixture.project.ID, "--task", "must stop", "--json"}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("missing canonical workspace unexpectedly succeeded")
	}
	if !strings.Contains(stdout.String(), handoffReasonCanonicalWorkspaceMissing) {
		t.Fatalf("missing canonical workspace was not reported: %s", stdout.String())
	}
}

func TestHandoffReportsMissingWorkspacePath(t *testing.T) {
	fixture := newHandoffFixture(t)
	value := core.Registry{SchemaVersion: 1, Projects: []core.ProjectRecord{fixture.project}, Repositories: []core.RepositoryRecord{fixture.repositoryRecord}, Workspaces: []core.WorkspaceRecord{fixture.workspace}}
	value.Workspaces[0].Path = filepath.Join(t.TempDir(), "missing")
	value.Workspaces[0].PathKey = workspace.PathKey(value.Workspaces[0].Path)
	if err := (registry.Store{Home: fixture.registryHome}).Save(value); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"handoff", fixture.project.ID, "--task", "must stop", "--json"}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("missing workspace unexpectedly succeeded")
	}
	if !strings.Contains(stdout.String(), handoffReasonWorkspaceMissing) {
		t.Fatalf("missing workspace was not reported: %s", stdout.String())
	}
}

func TestHandoffReportsStaleStateAndMissingVerificationContract(t *testing.T) {
	fixture := newHandoffFixture(t)
	if err := os.Remove(filepath.Join(fixture.repository, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.repository, "docs", "agent", "START-HERE.md")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"handoff", fixture.project.ID, "--task", "review", "--agent", "claude", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("stale handoff failed instead of warning: %s", stderr.String())
	}
	var result handoffResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != handoffReadyWithIssues || result.Handoff == nil || !hasHandoffReason(result.Warnings, handoffReasonNoVerificationContract) || !hasHandoffReason(result.Warnings, handoffReasonStateStale) {
		t.Fatalf("stale or missing-contract state was not reported: %#v", result)
	}
	if len(result.Handoff.AgentHints.Notes) == 0 || !strings.Contains(result.Handoff.AgentHints.Notes[0], "@AGENTS.md") {
		t.Fatalf("claude adapter hint was not thin and explicit: %#v", result.Handoff.AgentHints)
	}
}

type handoffFixture struct {
	registryHome     string
	repository       string
	project          core.ProjectRecord
	repositoryRecord core.RepositoryRecord
	workspace        core.WorkspaceRecord
}

func newHandoffFixture(t *testing.T) handoffFixture {
	t.Helper()
	repository := filepath.Join(t.TempDir(), "pilot")
	if err := os.MkdirAll(filepath.Join(repository, "docs", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repository, "init", "-b", "main")
	runTestGit(t, repository, "remote", "add", "origin", "https://user:secret@github.com/ukyovfx/pilot.git")
	if err := os.WriteFile(filepath.Join(repository, "AGENTS.md"), []byte("# Instructions\n\n## Verification\n- go test ./...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "docs", "agent", "START-HERE.md"), []byte("# Start\n\n## Verification route\n- Unit tests: `go test ./...`\n- Static checks: `go vet ./...`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "product.txt"), []byte("pilot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repository, "add", ".")
	runTestGit(t, repository, "commit", "-m", "product")
	basis := strings.TrimSpace(string(runGitOutput(t, repository, "rev-parse", "HEAD")))
	currentState := "---\ncontextbridge_state_schema: 1\nbasis_branch: main\nbasis_commit: " + basis + "\nbasis_date: " + time.Now().UTC().Format(time.RFC3339) + "\n---\n# Current State\n"
	if err := os.WriteFile(filepath.Join(repository, "docs", "agent", "CURRENT-STATE.md"), []byte(currentState), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repository, "add", "docs/agent/CURRENT-STATE.md")
	runTestGit(t, repository, "commit", "-m", "state metadata")
	probe, err := (workspace.Prober{}).Probe(repository)
	if err != nil {
		t.Fatal(err)
	}
	project := core.ProjectRecord{ID: "prj_aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", DisplayName: "Pilot", Aliases: []string{"pilot"}}
	repositoryRecord := core.RepositoryRecord{ID: "repo_bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", ProjectID: project.ID, Identity: *probe.PrimaryRemote, PrimaryRemoteName: "origin", CanonicalBranch: "main"}
	registered := workspaceRecord("ws_cccccccc-cccc-4ccc-8ccc-cccccccccccc", repositoryRecord.ID, core.RoleCanonical, probe)
	registryHome := filepath.Join(t.TempDir(), "registry")
	if err := (registry.Store{Home: registryHome}).Save(core.Registry{SchemaVersion: 1, Projects: []core.ProjectRecord{project}, Repositories: []core.RepositoryRecord{repositoryRecord}, Workspaces: []core.WorkspaceRecord{registered}}); err != nil {
		t.Fatal(err)
	}
	return handoffFixture{registryHome: registryHome, repository: repository, project: project, repositoryRecord: repositoryRecord, workspace: registered}
}

func hasHandoffReason(issues []handoffIssue, expected string) bool {
	for _, issue := range issues {
		if issue.Reason == expected {
			return true
		}
	}
	return false
}

func runGitOutput(t *testing.T, directory string, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	data, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return data
}
