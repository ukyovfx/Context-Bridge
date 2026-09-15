package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func runGuard(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("guard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectSelector := flags.String("project", "", "project id or alias")
	workspaceSelector := flags.String("workspace", "", "workspace id or path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *projectSelector == "" {
		return errors.New("guard requires --project <id-or-alias>")
	}
	store, err := registryStore()
	if err != nil {
		return err
	}
	value, err := store.Load()
	if err != nil {
		return err
	}
	project, err := resolveProject(value, *projectSelector)
	if err != nil {
		return failGuard(stderr, core.ReasonProjectNotFound)
	}
	repository, err := repositoryForProject(value, project.ID)
	if err != nil {
		return failGuard(stderr, core.ReasonRepositoryMismatch)
	}
	registered, actualPath, err := selectWorkspace(value, repository.ID, *workspaceSelector)
	if err != nil {
		return failGuard(stderr, core.ReasonWorkspacePathMismatch)
	}
	probe, probeStatus, probeErr := (workspace.Prober{PrimaryRemoteName: repository.PrimaryRemoteName}).ProbeWithStatus(actualPath)
	if probeErr != nil || probeStatus != workspace.ProbeOK {
		return failProbe(stderr, string(probeStatus))
	}
	expected := guardExpected(project, repository, registered, "")
	decision := core.EvaluateGuard(expected, core.GuardActual{ProjectFound: true, ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, Probe: probe})
	if !decision.Allowed {
		fmt.Fprintf(stderr, "Guard: BLOCKED\nReason: %s\nDetails: %v\nAction: use the registered canonical workspace and resolve the reported identity mismatch.\n", decision.PublicCode, decision.Reasons)
		return core.WrongWorkspaceError{Reasons: decision.Reasons}
	}
	fmt.Fprintf(stdout, "Project: %s\nWorkspace: %s\nGuard: PASS\n", project.DisplayName, registered.Path)
	return nil
}

func selectWorkspace(value core.Registry, repositoryID, selector string) (core.WorkspaceRecord, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return core.WorkspaceRecord{}, "", err
	}
	actualPath := cwd
	if selector != "" && (filepath.IsAbs(selector) || selector == ".") {
		if err := workspace.ValidateIdentityPath(selector); err != nil {
			return core.WorkspaceRecord{}, "", err
		}
		actualPath, err = workspace.CanonicalPath(selector)
		if err != nil {
			return core.WorkspaceRecord{}, "", err
		}
	}
	actualKey := workspace.PathKey(actualPath)
	matches := make([]core.WorkspaceRecord, 0)
	for _, registered := range value.Workspaces {
		if registered.RepositoryID != repositoryID {
			continue
		}
		if selector == "" {
			if registered.Role == core.RoleCanonical {
				matches = append(matches, registered)
			}
		} else if registered.ID == selector || registered.PathKey == actualKey {
			matches = append(matches, registered)
		}
	}
	if len(matches) != 1 {
		return core.WorkspaceRecord{}, "", errors.New("workspace selector is missing or ambiguous")
	}
	return matches[0], actualPath, nil
}

func guardExpected(project core.ProjectRecord, repository core.RepositoryRecord, registered core.WorkspaceRecord, fingerprint string) core.GuardExpected {
	return core.GuardExpected{ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, PathKey: registered.PathKey, GitRootKey: registered.GitRootKey, GitDirKey: registered.GitDirKey, GitCommonDirKey: registered.GitCommonDirKey, PhysicalIdentity: registered.PhysicalIdentity, PrimaryRemote: repository.Identity, BranchPolicy: core.BranchPolicy{Branch: repository.CanonicalBranch}, PlannedFingerprint: fingerprint}
}

func failGuard(stderr io.Writer, reason core.GuardReason) error {
	fmt.Fprintf(stderr, "Guard: BLOCKED\nReason: %s\nAction: resolve the project or workspace selection and retry.\n", reason)
	return core.WrongWorkspaceError{Reasons: []core.GuardReason{reason}}
}

func failProbe(stderr io.Writer, reason string) error {
	if reason == "" {
		reason = string(workspace.ProbeGitProbeFailed)
	}
	fmt.Fprintf(stderr, "Guard: BLOCKED\nReason: %s\nAction: verify that the target exists and that Git can read it without changing Git configuration.\n", reason)
	return errors.New(reason)
}

type liveGuardVerifier struct {
	Project    core.ProjectRecord
	Repository core.RepositoryRecord
	Workspace  core.WorkspaceRecord
	Path       string
	Prober     workspace.Prober
}

func (v liveGuardVerifier) Verify(expected core.GuardExpected) error {
	prober := v.Prober
	if prober.Runner == nil {
		prober = workspace.Prober{PrimaryRemoteName: v.Repository.PrimaryRemoteName}
	}
	probe, err := prober.Probe(v.Path)
	if err != nil {
		return core.WrongWorkspaceError{Reasons: []core.GuardReason{core.ReasonIdentityChangedAfterPlan}}
	}
	decision := core.EvaluateGuard(expected, core.GuardActual{ProjectFound: true, ProjectID: v.Project.ID, RepositoryID: v.Repository.ID, WorkspaceID: v.Workspace.ID, Probe: probe})
	if !decision.Allowed {
		return core.WrongWorkspaceError{Reasons: decision.Reasons}
	}
	return nil
}
