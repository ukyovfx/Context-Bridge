package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/contextscan"
	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/idgen"
)

type integrationPlan struct {
	Command                 string               `json:"command"`
	Status                  string               `json:"status"`
	Reason                  string               `json:"reason,omitempty"`
	Target                  string               `json:"target"`
	Project                 string               `json:"project"`
	RepositoryIdentity      core.RemoteIdentity  `json:"repository_identity"`
	Manifest                migrationPlan        `json:"manifest"`
	Context                 contextscan.Proposal `json:"context"`
	Registration            string               `json:"registration"`
	Warnings                []string             `json:"warnings"`
	RequiresConfirmation    bool                 `json:"requires_confirmation"`
	PreconditionFingerprint string               `json:"precondition_fingerprint"`
}

var integrateRevalidate = revalidateRepository

func runIntegrate(args []string, stdout, stderr io.Writer, version string) error {
	path, remaining, err := takePath(args, "integrate")
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("integrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "print the integration plan without mutating")
	confirm := flags.Bool("confirm", false, "confirm repository file changes")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(remaining); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}

	plan, data, probe, registryValue, err := buildIntegrationPlan(path, version)
	if err != nil {
		emitIntegrationPlan(stdout, plan, *jsonOutput)
		return err
	}
	if len(plan.Warnings) > 0 {
		emitIntegrationPlan(stdout, plan, *jsonOutput)
		return fmt.Errorf("integration blocked by repository context conflict: %s", strings.Join(plan.Warnings, ", "))
	}
	if plan.Status == "already_integrated" {
		emitIntegrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if *dryRun {
		plan.Status = "planned"
		emitIntegrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if plan.RequiresConfirmation && !*confirm {
		plan.Status = "confirmation_required"
		plan.Reason = reasonConfirmationRequired
		emitIntegrationPlan(stdout, plan, *jsonOutput)
		return errors.New("confirmation required; review the integration plan and rerun with --confirm")
	}
	if err := integrateRevalidate(probe.GitRoot, plan.PreconditionFingerprint); err != nil {
		plan.Status = "failed"
		plan.Reason = reasonIdentityChangedAfterPlan
		emitIntegrationPlan(stdout, plan, *jsonOutput)
		return err
	}
	if err := applyIntegrationFiles(plan, data); err != nil {
		return err
	}
	if plan.Registration != "already_registered" {
		if err := saveIntegrationRegistry(registryValue, probe, plan); err != nil {
			return err
		}
	}
	plan.Status = "applied"
	emitIntegrationPlan(stdout, plan, *jsonOutput)
	if !*jsonOutput {
		fmt.Fprintf(stdout, "registered project: %s\ncanonical workspace: %s\nContext Bridge readiness: ready\n", plan.Project, plan.Target)
		if plan.Context.Reason == "PRESERVE_EXISTING_CONTEXT" && len(plan.Context.Changes) == 0 {
			fmt.Fprintln(stdout, "context: existing repository-native context preserved")
		}
	}
	return nil
}

func buildIntegrationPlan(path, version string) (integrationPlan, []byte, core.WorkspaceProbe, core.Registry, error) {
	root, probe, err := probeMigrationTarget(path)
	plan := integrationPlan{Command: "integrate", Status: "failed", Target: root, Warnings: []string{}}
	if err != nil {
		plan.Reason = err.Error()
		return plan, nil, core.WorkspaceProbe{}, core.Registry{}, err
	}
	plan.Target = root
	plan.PreconditionFingerprint = probe.Fingerprint
	plan.RepositoryIdentity = *probe.PrimaryRemote
	inspection, err := contextscan.Inspect(root)
	if err != nil {
		plan.Reason = err.Error()
		return plan, nil, probe, core.Registry{}, err
	}
	contextProposal, err := contextscan.Preview(root)
	if err != nil {
		plan.Reason = err.Error()
		return plan, nil, probe, core.Registry{}, err
	}
	plan.Context = contextProposal
	if len(inspection.Conflicts) > 0 || inspection.Agents == "conflict" {
		plan.Warnings = append(plan.Warnings, inspection.Conflicts...)
		if inspection.Agents == "conflict" {
			plan.Warnings = append(plan.Warnings, "AGENTS_CONFLICT")
		}
		plan.Reason = "CONFLICTING_AGENT_INSTRUCTIONS"
		return plan, nil, probe, core.Registry{}, errors.New(plan.Reason)
	}

	manifestPlan, manifestData, _, _, err := buildAdoptPlan(root, version)
	if err != nil {
		plan.Manifest = manifestPlan
		plan.Reason = err.Error()
		return plan, nil, probe, core.Registry{}, err
	}
	plan.Manifest = manifestPlan
	data := manifestData
	projectName := filepath.Base(root)
	projectID := manifestPlan.Proposed.ProjectID
	repositoryID := manifestPlan.Proposed.RepositoryID
	if manifestPlan.Status == "already_adopted" {
		manifest, readErr := readManifest(root)
		if readErr != nil || manifest.Repository == nil || !manifest.Repository.Identity.Equal(*probe.PrimaryRemote) {
			plan.Reason = reasonRepositoryMismatch
			return plan, nil, probe, core.Registry{}, errors.New(plan.Reason)
		}
		projectName, projectID, repositoryID = manifest.Project.Name, manifest.Project.ID, manifest.Repository.ID
		plan.Manifest.Proposed.ProjectID = projectID
		plan.Manifest.Proposed.RepositoryID = repositoryID
	}
	plan.Project = projectName
	store, err := registryStore()
	if err != nil {
		plan.Reason = err.Error()
		return plan, nil, probe, core.Registry{}, err
	}
	registryValue, err := store.Load()
	if err != nil {
		plan.Reason = err.Error()
		return plan, nil, probe, core.Registry{}, err
	}
	registration, err := planIntegrationRegistration(&registryValue, probe, projectName, projectID, repositoryID)
	if err != nil {
		plan.Reason = err.Error()
		return plan, data, probe, registryValue, err
	}
	plan.Registration = registration
	if registration != "already_registered" {
		plan.RequiresConfirmation = len(contextProposal.Changes) > 0 || len(data) > 0
	}
	if manifestPlan.Status == "already_adopted" && len(contextProposal.Changes) == 0 && registration == "already_registered" {
		plan.Status = "already_integrated"
	}
	return plan, data, probe, registryValue, nil
}

func planIntegrationRegistration(value *core.Registry, probe core.WorkspaceProbe, projectName, projectID, repositoryID string) (string, error) {
	for _, registered := range value.Workspaces {
		if registered.PathKey == probe.PathKey {
			if registered.RepositoryID == repositoryID {
				return "already_registered", nil
			}
			return "", errors.New("registry conflict: workspace path is registered to another repository")
		}
	}
	for _, repository := range value.Repositories {
		if repository.Identity.Equal(*probe.PrimaryRemote) {
			if repository.ID != repositoryID || repository.ProjectID != projectID {
				return "", errors.New("registry conflict: repository identity has different project identifiers")
			}
			for _, registered := range value.Workspaces {
				if registered.RepositoryID == repository.ID && registered.Role == core.RoleCanonical {
					return "", errors.New("registry conflict: repository already has another canonical workspace")
				}
			}
			return "register_canonical", nil
		}
	}
	for _, project := range value.Projects {
		if project.ID == projectID || project.DisplayName == projectName || contains(project.Aliases, projectName) {
			return "", errors.New("registry conflict: project identity is already in use")
		}
	}
	return "register_canonical", nil
}

func applyIntegrationFiles(plan integrationPlan, manifestData []byte) error {
	for _, change := range plan.Context.Changes {
		path := filepath.Join(plan.Target, filepath.FromSlash(change.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if filepath.ToSlash(change.Path) == "AGENTS.md" {
			current, readErr := os.ReadFile(path)
			if readErr == nil {
				if string(current) == change.Content || strings.Contains(strings.ToLower(string(current)), "context bridge") {
					continue
				}
				if !strings.HasPrefix(change.Content, string(current)) {
					return errors.New("AGENTS.md changed after integration preview; generate a new preview")
				}
				file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					return fmt.Errorf("refusing to update AGENTS.md: %w", err)
				}
				_, writeErr := file.WriteString(strings.TrimPrefix(change.Content, string(current)))
				closeErr := file.Close()
				if writeErr != nil {
					return writeErr
				}
				if closeErr != nil {
					return closeErr
				}
				continue
			}
			if !errors.Is(readErr, os.ErrNotExist) {
				return readErr
			}
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		file, err := os.OpenFile(path, flags, 0o644)
		if err != nil {
			return fmt.Errorf("refusing to update %s: %w", change.Path, err)
		}
		_, writeErr := file.WriteString(change.Content)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if len(manifestData) > 0 {
		path := filepath.Join(plan.Target, ".contextbridge", "manifest.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("refusing to create manifest: %w", err)
		}
		_, writeErr := file.Write(manifestData)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func saveIntegrationRegistry(value core.Registry, probe core.WorkspaceProbe, plan integrationPlan) error {
	store, err := registryStore()
	if err != nil {
		return err
	}
	current, err := store.Load()
	if err != nil {
		return err
	}
	if _, err := planIntegrationRegistration(&current, probe, plan.Project, plan.Manifest.Proposed.ProjectID, plan.Manifest.Proposed.RepositoryID); err != nil {
		return err
	}
	if plan.Manifest.Proposed.ProjectID == "" || plan.Manifest.Proposed.RepositoryID == "" {
		return errors.New("integration manifest identifiers are missing")
	}
	projectID := plan.Manifest.Proposed.ProjectID
	repositoryID := plan.Manifest.Proposed.RepositoryID
	for _, repository := range current.Repositories {
		if repository.Identity.Equal(*probe.PrimaryRemote) {
			projectID = repository.ProjectID
			repositoryID = repository.ID
			break
		}
	}
	if _, err := projectForRepository(current, repositoryID); err != nil {
		current.Projects = append(current.Projects, core.ProjectRecord{ID: projectID, DisplayName: plan.Project})
		current.Repositories = append(current.Repositories, core.RepositoryRecord{ID: repositoryID, ProjectID: projectID, Identity: *probe.PrimaryRemote, PrimaryRemoteName: probe.PrimaryRemoteName, CanonicalBranch: probe.Branch})
	}
	workspaceID, err := idgen.New(core.WorkspaceIDPrefix)
	if err != nil {
		return err
	}
	current.Workspaces = append(current.Workspaces, workspaceRecord(workspaceID, repositoryID, core.RoleCanonical, probe))
	return store.Save(current)
}

func emitIntegrationPlan(stdout io.Writer, plan integrationPlan, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.Marshal(plan)
		fmt.Fprintln(stdout, string(data))
		return
	}
	fmt.Fprintf(stdout, "integration %s: %s\n", plan.Status, plan.Target)
	if plan.Project != "" {
		fmt.Fprintf(stdout, "project: %s\n", plan.Project)
	}
	if plan.Manifest.Status == "planned" {
		fmt.Fprintln(stdout, "change: create .contextbridge/manifest.json")
	}
	for _, change := range plan.Context.Changes {
		fmt.Fprintf(stdout, "change: update %s\n", change.Path)
	}
	if plan.Registration != "" {
		fmt.Fprintf(stdout, "registration: %s\n", plan.Registration)
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintf(stdout, "warning: %s\n", warning)
	}
	if plan.Reason != "" {
		fmt.Fprintf(stdout, "reason: %s\n", plan.Reason)
	}
}
