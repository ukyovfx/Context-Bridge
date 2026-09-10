package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
	"github.com/ukyovfx/Context-Bridge/internal/state"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func runDoctor(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectPath := flags.String("project", "", "Context Bridge project path")
	agent := flags.String("agent", "", "codex, claude, or cursor instruction diagnostics")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("doctor accepts no positional arguments")
	}
	if *agent != "" && !validAgent(*agent) {
		return errors.New("agent must be codex, claude, or cursor")
	}
	if *projectPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		*projectPath = cwd
	}
	absProject, err := filepath.Abs(*projectPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(absProject, ".contextbridge", "manifest.json"))
	if err != nil {
		return errors.New("not a Context Bridge-created project: manifest missing or unreadable")
	}
	manifest, err := core.ParseManifest(data)
	if err != nil {
		return err
	}
	currentStateHashOK := false
	for _, item := range manifest.Files {
		path, err := filepathWithin(absProject, item.Path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("managed file missing: %s", item.Path)
		}
		sum := sha256.Sum256(contents)
		if hex.EncodeToString(sum[:]) != item.SHA256 {
			if filepath.ToSlash(item.Path) == "docs/agent/CURRENT-STATE.md" {
				return errors.New("CURRENT-STATE content integrity mismatch")
			}
			return fmt.Errorf("managed file changed: %s", item.Path)
		}
		if filepath.ToSlash(item.Path) == "docs/agent/CURRENT-STATE.md" {
			currentStateHashOK = true
		}
	}
	primaryRemote := "origin"
	if manifest.SchemaVersion == 2 && manifest.Repository.PrimaryRemoteName != "" {
		primaryRemote = manifest.Repository.PrimaryRemoteName
	}
	probe, probeStatus, probeErr := (workspace.Prober{PrimaryRemoteName: primaryRemote}).ProbeWithStatus(absProject)
	if probeErr != nil || probeStatus != workspace.ProbeOK {
		if probeErr != nil {
			return fmt.Errorf("Git probe failed: %s", probeStatus)
		}
		return errors.New(string(probeStatus))
	}
	if probe.GitRootKey != probe.PathKey {
		return errors.New("Git root does not match the project path")
	}
	if !manifest.Lifecycle.LocalOnly {
		if probe.PrimaryRemote == nil {
			return errors.New("primary remote is missing or ambiguous")
		}
		expected := core.RemoteIdentity{Host: "github.com", Path: manifest.Project.Repository}
		if manifest.SchemaVersion == 2 {
			expected = manifest.Repository.Identity
		}
		if !expected.Equal(*probe.PrimaryRemote) {
			return errors.New("primary remote identity does not match manifest")
		}
	}
	if manifest.SchemaVersion == 2 && (probe.Detached || probe.Branch != manifest.Repository.CanonicalBranch) {
		return errors.New("workspace branch does not match manifest canonical branch")
	}
	report := state.Evaluate(absProject, workspace.CommandRunner{})
	if manifest.SchemaVersion == 1 && currentStateHashOK && report.ContentIntegrity == "unverified" {
		report.ContentIntegrity = "ok"
	}
	fmt.Fprintf(stdout, "doctor ok: manifest v%d; %d generated-file hashes verified\n", manifest.SchemaVersion, len(manifest.Files))
	fmt.Fprintf(stdout, "content integrity: %s\n", report.ContentIntegrity)
	fmt.Fprintf(stdout, "basis validity: %s\n", report.BasisValidity)
	fmt.Fprintf(stdout, "basis freshness: %s\n", report.BasisFreshness)
	if len(report.Reasons) > 0 {
		fmt.Fprintf(stdout, "basis evidence: %v\n", report.Reasons)
	}
	if report.ContentIntegrity == "modified" {
		return errors.New("CURRENT-STATE has uncommitted content changes")
	}
	if manifest.SchemaVersion == 2 {
		if err := doctorRegisteredGuard(manifest, probe); err != nil {
			return err
		}
	}
	if *agent != "" {
		diagnostics := diagnoseAgent(absProject, *agent)
		emitInstructionResult(stdout, diagnostics, false)
		if diagnostics.Status == readinessFailed {
			return errors.New("instruction diagnostics failed")
		}
	}
	return nil
}

func filepathWithin(root, relative string) (string, error) {
	return safety.JoinWithin(root, relative)
}

func doctorRegisteredGuard(manifest core.Manifest, probe core.WorkspaceProbe) error {
	store, err := registryStore()
	if err != nil {
		return err
	}
	value, err := store.Load()
	if err != nil {
		return err
	}
	for _, registered := range value.Workspaces {
		if registered.PathKey != probe.PathKey || registered.RepositoryID != manifest.Repository.ID {
			continue
		}
		project, err := resolveProject(value, manifest.Project.ID)
		if err != nil {
			return core.WrongWorkspaceError{Reasons: []core.GuardReason{core.ReasonProjectNotFound}}
		}
		repository, err := repositoryForProject(value, project.ID)
		if err != nil {
			return core.WrongWorkspaceError{Reasons: []core.GuardReason{core.ReasonRepositoryMismatch}}
		}
		decision := core.EvaluateGuard(guardExpected(project, repository, registered, ""), core.GuardActual{ProjectFound: true, ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, Probe: probe})
		if !decision.Allowed {
			return core.WrongWorkspaceError{Reasons: decision.Reasons}
		}
		return nil
	}
	return nil
}
