package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/state"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

type handoffStatus string

const (
	handoffReady           handoffStatus = "ready"
	handoffReadyWithIssues handoffStatus = "ready_with_warnings"
	handoffFailed          handoffStatus = "failed"
)

const (
	handoffReasonProjectNotFound           = "PROJECT_NOT_FOUND"
	handoffReasonProjectAmbiguous          = "PROJECT_AMBIGUOUS"
	handoffReasonCanonicalWorkspaceMissing = "CANONICAL_WORKSPACE_NOT_REGISTERED"
	handoffReasonWorkspaceMissing          = "WORKSPACE_MISSING"
	handoffReasonWrongWorkspace            = "WRONG_WORKSPACE"
	handoffReasonStateStale                = "STATE_STALE"
	handoffReasonNoVerificationContract    = "NO_VERIFICATION_CONTRACT"
	handoffReasonProbeFailed               = "PROBE_FAILED"
	handoffReasonVerificationUnverified    = "UNVERIFIED"
)

type handoffIssue struct {
	Reason string `json:"reason"`
}

type handoffGuard struct {
	Allowed    bool     `json:"allowed"`
	PublicCode string   `json:"public_code,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
}

type verificationContract struct {
	Status               string       `json:"status"`
	Source               []string     `json:"source,omitempty"`
	RequiredCommands     []string     `json:"required_commands,omitempty"`
	CompletionConditions []string     `json:"completion_conditions,omitempty"`
	CurrentState         state.Report `json:"current_state"`
	Reasons              []string     `json:"reasons,omitempty"`
}

type agentHints struct {
	ExpectedCWD          string   `json:"expected_cwd"`
	ContextEntrypoints   []string `json:"context_entrypoints,omitempty"`
	VerificationCommands []string `json:"verification_commands,omitempty"`
	Notes                []string `json:"notes,omitempty"`
}

type handoffDocument struct {
	SchemaVersion      int                  `json:"schema_version"`
	Agent              string               `json:"agent"`
	ProjectID          string               `json:"project_id"`
	ProjectName        string               `json:"project_name"`
	RepositoryID       string               `json:"repository_id"`
	WorkspaceID        string               `json:"workspace_id"`
	WorkspacePath      string               `json:"workspace_path"`
	GitRoot            string               `json:"git_root"`
	Branch             string               `json:"branch,omitempty"`
	Detached           bool                 `json:"detached"`
	Head               string               `json:"head,omitempty"`
	BaseCommit         string               `json:"base_commit"`
	RepositoryIdentity core.RemoteIdentity  `json:"repository_identity"`
	ContextEntrypoints []string             `json:"context_entrypoints,omitempty"`
	AcceptedState      state.Report         `json:"accepted_state"`
	TaskIntent         string               `json:"task_intent"`
	Verification       verificationContract `json:"verification"`
	Guard              handoffGuard         `json:"guard"`
	AgentDiagnostics   agentDiagnostics     `json:"agent_diagnostics"`
	AgentHints         agentHints           `json:"agent_hints"`
	GeneratedAt        string               `json:"generated_at"`
}

type handoffResult struct {
	Status     handoffStatus    `json:"status"`
	Project    string           `json:"project,omitempty"`
	TaskIntent string           `json:"task_intent,omitempty"`
	Warnings   []handoffIssue   `json:"warnings"`
	Errors     []handoffIssue   `json:"errors"`
	Handoff    *handoffDocument `json:"handoff,omitempty"`
}

type handoffResolutionError struct {
	Reason  string
	Matches []core.ProjectRecord
}

func (e handoffResolutionError) Error() string { return e.Reason }

func runHandoff(args []string, stdout, stderr io.Writer) error {
	projectSelector := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		projectSelector = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("handoff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	task := flags.String("task", "", "task intent")
	agent := flags.String("agent", "codex", "codex, claude, or cursor")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if projectSelector == "" && flags.NArg() == 1 {
		projectSelector = flags.Arg(0)
	}
	if projectSelector == "" || flags.NArg() > 1 || strings.TrimSpace(*task) == "" {
		return errors.New("handoff requires <project> and --task <intent>")
	}
	if *agent != "codex" && *agent != "claude" && *agent != "cursor" {
		return errors.New("agent must be codex, claude, or cursor")
	}

	result := handoffResult{Status: handoffFailed, Project: projectSelector, TaskIntent: *task, Warnings: []handoffIssue{}, Errors: []handoffIssue{}}
	store, err := registryStore()
	if err != nil {
		result.Errors = append(result.Errors, handoffIssue{Reason: handoffReasonProbeFailed})
		emitHandoffResult(stdout, result, *jsonOutput)
		return err
	}
	value, err := store.Load()
	if err != nil {
		result.Errors = append(result.Errors, handoffIssue{Reason: handoffReasonProbeFailed})
		emitHandoffResult(stdout, result, *jsonOutput)
		return err
	}

	project, err := resolveHandoffProject(value, projectSelector)
	if err != nil {
		var resolution handoffResolutionError
		if errors.As(err, &resolution) {
			result.Errors = append(result.Errors, handoffIssue{Reason: resolution.Reason})
			if !*jsonOutput && len(resolution.Matches) > 0 {
				for _, match := range resolution.Matches {
					fmt.Fprintf(stdout, "match: %s\t%s\n", match.ID, match.DisplayName)
				}
			}
		}
		emitHandoffResult(stdout, result, *jsonOutput)
		return err
	}
	result.Project = project.DisplayName
	repository, err := repositoryForProject(value, project.ID)
	if err != nil {
		result.Errors = append(result.Errors, handoffIssue{Reason: handoffReasonProbeFailed})
		emitHandoffResult(stdout, result, *jsonOutput)
		return err
	}
	registered, ok := canonicalWorkspace(value, repository.ID)
	if !ok {
		result.Errors = append(result.Errors, handoffIssue{Reason: handoffReasonCanonicalWorkspaceMissing})
		emitHandoffResult(stdout, result, *jsonOutput)
		return errors.New(handoffReasonCanonicalWorkspaceMissing)
	}
	if _, err := os.Stat(registered.Path); err != nil {
		reason := handoffReasonWorkspaceMissing
		if !errors.Is(err, os.ErrNotExist) {
			reason = handoffReasonProbeFailed
		}
		result.Errors = append(result.Errors, handoffIssue{Reason: reason})
		emitHandoffResult(stdout, result, *jsonOutput)
		return errors.New(reason)
	}
	canonical, err := workspace.CanonicalPath(registered.Path)
	if err != nil || workspace.PathKey(canonical) != registered.PathKey {
		result.Errors = append(result.Errors, handoffIssue{Reason: handoffReasonWrongWorkspace})
		emitHandoffResult(stdout, result, *jsonOutput)
		return errors.New(handoffReasonWrongWorkspace)
	}
	probe, err := (workspace.Prober{PrimaryRemoteName: repository.PrimaryRemoteName}).Probe(canonical)
	if err != nil {
		result.Errors = append(result.Errors, handoffIssue{Reason: handoffReasonProbeFailed})
		emitHandoffResult(stdout, result, *jsonOutput)
		return err
	}
	expected := guardExpected(project, repository, registered, "")
	decision := core.EvaluateGuard(expected, core.GuardActual{ProjectFound: true, ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, Probe: probe})
	guard := handoffGuard{Allowed: decision.Allowed, PublicCode: decision.PublicCode}
	for _, reason := range decision.Reasons {
		guard.Reasons = append(guard.Reasons, string(reason))
	}
	if !decision.Allowed {
		result.Errors = append(result.Errors, handoffIssue{Reason: handoffReasonWrongWorkspace})
		emitHandoffResult(stdout, result, *jsonOutput)
		return core.WrongWorkspaceError{Reasons: decision.Reasons}
	}

	root := probe.GitRoot
	entrypoints := contextEntrypoints(root)
	verification, baseCommit := resolveVerification(root, entrypoints)
	diagnosticResult := diagnoseAgent(canonical, *agent)
	for _, issue := range diagnosticResult.Diagnostics.Warnings {
		result.Warnings = append(result.Warnings, handoffIssue{Reason: issue.Reason})
	}
	if verification.Status == "UNVERIFIED" {
		result.Warnings = append(result.Warnings, handoffIssue{Reason: handoffReasonNoVerificationContract})
	}
	if verification.CurrentState.BasisFreshness == "stale" || verification.CurrentState.ContentIntegrity == "modified" {
		result.Warnings = append(result.Warnings, handoffIssue{Reason: handoffReasonStateStale})
	}
	if len(probe.EvidenceErrors) > 0 {
		result.Warnings = append(result.Warnings, handoffIssue{Reason: handoffReasonVerificationUnverified})
	}
	document := makeHandoffDocument(project, repository, registered, probe, verification, baseCommit, *task, *agent, entrypoints, guard, diagnosticResult.Diagnostics)
	result.Handoff = &document
	result.Status = handoffReady
	if len(result.Warnings) > 0 {
		result.Status = handoffReadyWithIssues
	}
	emitHandoffResult(stdout, result, *jsonOutput)
	return nil
}

func resolveHandoffProject(value core.Registry, selector string) (core.ProjectRecord, error) {
	matches := make([]core.ProjectRecord, 0)
	for _, project := range value.Projects {
		if project.ID == selector || project.DisplayName == selector || contains(project.Aliases, selector) {
			matches = append(matches, project)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return core.ProjectRecord{}, handoffResolutionError{Reason: handoffReasonProjectNotFound}
	default:
		return core.ProjectRecord{}, handoffResolutionError{Reason: handoffReasonProjectAmbiguous, Matches: matches}
	}
}

func canonicalWorkspace(value core.Registry, repositoryID string) (core.WorkspaceRecord, bool) {
	for _, registered := range value.Workspaces {
		if registered.RepositoryID == repositoryID && registered.Role == core.RoleCanonical {
			return registered, true
		}
	}
	return core.WorkspaceRecord{}, false
}

func contextEntrypoints(root string) []string {
	candidates := []string{"AGENTS.md", filepath.Join("docs", "agent", "START-HERE.md"), filepath.Join("docs", "agent", "CURRENT-STATE.md")}
	entrypoints := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(root, candidate)); err == nil && !info.IsDir() {
			entrypoints = append(entrypoints, filepath.ToSlash(candidate))
		}
	}
	return entrypoints
}

func resolveVerification(root string, entrypoints []string) (verificationContract, string) {
	contract := verificationContract{Status: handoffReasonVerificationUnverified, CurrentState: state.Evaluate(root, workspace.CommandRunner{})}
	for _, entrypoint := range entrypoints {
		if entrypoint != filepath.ToSlash(filepath.Join("docs", "agent", "START-HERE.md")) && entrypoint != "AGENTS.md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entrypoint)))
		if err != nil {
			continue
		}
		commands := verificationCommands(string(data))
		if len(commands) == 0 {
			continue
		}
		contract.Status = "RESOLVED"
		contract.Source = append(contract.Source, entrypoint)
		contract.RequiredCommands = commands
		contract.CompletionConditions = []string{"all required commands complete successfully", "current accepted-state evidence remains valid"}
		break
	}
	if contract.Status == handoffReasonVerificationUnverified {
		contract.Reasons = []string{handoffReasonNoVerificationContract}
	}
	baseCommit := ""
	if provenance, err := state.Parse(filepath.Join(root, "docs", "agent", "CURRENT-STATE.md")); err == nil {
		baseCommit = provenance.BasisCommit
	}
	return contract, baseCommit
}

func verificationCommands(contents string) []string {
	lines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")
	inRoute := false
	commands := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			inRoute = strings.EqualFold(heading, "verification route") || strings.EqualFold(heading, "verification") || strings.EqualFold(heading, "required verification")
			continue
		}
		if !inRoute || !strings.HasPrefix(trimmed, "-") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if colon := strings.Index(value, ":"); colon >= 0 {
			value = strings.TrimSpace(value[colon+1:])
		}
		value = strings.Trim(value, "`")
		if value != "" {
			commands = append(commands, value)
		}
	}
	return commands
}

func makeHandoffDocument(project core.ProjectRecord, repository core.RepositoryRecord, registered core.WorkspaceRecord, probe core.WorkspaceProbe, verification verificationContract, baseCommit, task, agent string, entrypoints []string, guard handoffGuard, diagnostics agentDiagnostics) handoffDocument {
	return handoffDocument{
		SchemaVersion: 1, Agent: agent, ProjectID: project.ID, ProjectName: project.DisplayName,
		RepositoryID: repository.ID, WorkspaceID: registered.ID, WorkspacePath: probe.CanonicalPath,
		GitRoot: probe.GitRoot, Branch: probe.Branch, Detached: probe.Detached, Head: probe.Head,
		BaseCommit: baseCommit, RepositoryIdentity: repository.Identity, ContextEntrypoints: entrypoints,
		AcceptedState: verification.CurrentState, TaskIntent: task, Verification: verification, Guard: guard, AgentDiagnostics: diagnostics,
		AgentHints:  makeAgentHints(agent, probe.CanonicalPath, entrypoints, verification.RequiredCommands),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func makeAgentHints(agent, cwd string, entrypoints, commands []string) agentHints {
	hints := agentHints{ExpectedCWD: cwd, ContextEntrypoints: entrypoints, VerificationCommands: commands}
	switch agent {
	case "claude":
		hints.Notes = []string{"Use @AGENTS.md when supported; do not rewrite agent configuration."}
	case "cursor":
		hints.Notes = []string{"Use the registered workspace root and existing context entrypoints."}
	default:
		hints.Notes = []string{"Use the expected working directory and existing AGENTS.md routing."}
	}
	return hints
}

func emitHandoffResult(stdout io.Writer, result handoffResult, jsonOutput bool) {
	if jsonOutput {
		data, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintln(stdout, `{"status":"failed","errors":[{"reason":"PROBE_FAILED"}],"warnings":[]}`)
			return
		}
		fmt.Fprintln(stdout, string(data))
		return
	}
	if result.Handoff != nil {
		fmt.Fprintf(stdout, "handoff status=%s project=%s repository=%s workspace=%s agent=%s\n", result.Status, result.Handoff.ProjectID, result.Handoff.RepositoryID, result.Handoff.WorkspaceID, result.Handoff.Agent)
		fmt.Fprintf(stdout, "workspace=%s branch=%s head=%s\n", result.Handoff.WorkspacePath, result.Handoff.Branch, result.Handoff.Head)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "warning reason=%s\n", warning.Reason)
	}
	for _, failure := range result.Errors {
		fmt.Fprintf(stdout, "error reason=%s\n", failure.Reason)
	}
	fmt.Fprintf(stdout, "handoff terminal_status=%s project=%s\n", result.Status, result.Project)
}
