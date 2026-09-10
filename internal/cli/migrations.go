package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/idgen"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

const (
	reasonProjectNotFound           = "PROJECT_NOT_FOUND"
	reasonProjectAmbiguous          = "PROJECT_AMBIGUOUS"
	reasonWorkspaceNotFound         = "WORKSPACE_NOT_FOUND"
	reasonWorkspaceAlreadyBound     = "WORKSPACE_ALREADY_BOUND"
	reasonRepositoryMismatch        = "REPOSITORY_MISMATCH"
	reasonTargetNotGit              = "TARGET_NOT_GIT"
	reasonTargetNotFound            = "TARGET_NOT_FOUND"
	reasonGitRepositoryInaccessible = "GIT_REPOSITORY_INACCESSIBLE"
	reasonGitProbeFailed            = "GIT_PROBE_FAILED"
	reasonManifestConflict          = "MANIFEST_CONFLICT"
	reasonAlreadyAdopted            = "ALREADY_ADOPTED"
	reasonAlreadyUpgraded           = "ALREADY_UPGRADED"
	reasonRebindAmbiguous           = "REBIND_AMBIGUOUS"
	reasonRebindIdentityMismatch    = "REBIND_IDENTITY_MISMATCH"
	reasonIdentityChangedAfterPlan  = "IDENTITY_CHANGED_AFTER_PLAN"
	reasonConfirmationRequired      = "CONFIRMATION_REQUIRED"
	reasonUnknownNewerManifest      = "UNKNOWN_NEWER_MANIFEST"
)

type migrationOperation struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Size   int    `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type migrationIdentity struct {
	ProjectID          string              `json:"project_id,omitempty"`
	RepositoryID       string              `json:"repository_id,omitempty"`
	WorkspaceID        string              `json:"workspace_id,omitempty"`
	Path               string              `json:"path,omitempty"`
	GitRoot            string              `json:"git_root,omitempty"`
	GitDir             string              `json:"git_dir,omitempty"`
	GitCommonDir       string              `json:"git_common_dir,omitempty"`
	Branch             string              `json:"branch,omitempty"`
	Head               string              `json:"head,omitempty"`
	Fingerprint        string              `json:"fingerprint,omitempty"`
	RepositoryIdentity core.RemoteIdentity `json:"repository_identity,omitempty"`
}

type migrationPlan struct {
	Command                 string               `json:"command"`
	Status                  string               `json:"status"`
	Reason                  string               `json:"reason,omitempty"`
	Target                  string               `json:"target,omitempty"`
	Current                 migrationIdentity    `json:"current"`
	Proposed                migrationIdentity    `json:"proposed"`
	PreconditionFingerprint string               `json:"precondition_fingerprint,omitempty"`
	RequiresConfirmation    bool                 `json:"requires_confirmation"`
	Operations              []migrationOperation `json:"operations"`
}

type manifestEnvelope struct {
	SchemaVersion int `json:"schema_version"`
}

func runAdopt(args []string, stdout, stderr io.Writer, version string) error {
	path, remaining, err := takePath(args, "adopt")
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("adopt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "print the adoption plan without mutating")
	confirm := flags.Bool("confirm", false, "confirm the planned adoption mutation")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(remaining); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	plan, manifestData, root, fingerprint, err := buildAdoptPlan(path, version)
	if err != nil {
		return emitMigrationFailure(stdout, planForFailure("adopt", path, err), *jsonOutput, err)
	}
	if plan.Status == "already_adopted" {
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if *dryRun {
		plan.Status = "planned"
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if !*confirm {
		plan.Status = "confirmation_required"
		plan.Reason = reasonConfirmationRequired
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return errors.New("confirmation required; rerun with --confirm")
	}
	if err := revalidateMutationTarget(root, fingerprint); err != nil {
		return emitMigrationFailure(stdout, planWithReason(plan, reasonIdentityChangedAfterPlan), *jsonOutput, err)
	}
	manifestPath := filepath.Join(root, ".contextbridge", "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return emitMigrationFailure(stdout, planWithReason(plan, reasonManifestConflict), *jsonOutput, errors.New(reasonManifestConflict))
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(manifestPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return emitMigrationFailure(stdout, planWithReason(plan, reasonManifestConflict), *jsonOutput, err)
	}
	_, writeErr := file.Write(manifestData)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	plan.Status = "applied"
	emitMigrationPlan(stdout, plan, *jsonOutput)
	return nil
}

func runUpgrade(args []string, stdout, stderr io.Writer, version string) error {
	path, remaining, err := takePath(args, "upgrade")
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "print the upgrade plan without mutating")
	confirm := flags.Bool("confirm", false, "confirm the planned upgrade mutation")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(remaining); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	plan, manifestData, backupData, root, fingerprint, err := buildUpgradePlan(path, version)
	if err != nil {
		return emitMigrationFailure(stdout, planForFailure("upgrade", path, err), *jsonOutput, err)
	}
	if plan.Status == "already_upgraded" {
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if *dryRun {
		plan.Status = "planned"
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if !*confirm {
		plan.Status = "confirmation_required"
		plan.Reason = reasonConfirmationRequired
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return errors.New("confirmation required; rerun with --confirm")
	}
	if err := revalidateMutationTarget(root, fingerprint); err != nil {
		return emitMigrationFailure(stdout, planWithReason(plan, reasonIdentityChangedAfterPlan), *jsonOutput, err)
	}
	manifestPath := filepath.Join(root, ".contextbridge", "manifest.json")
	backupPath := manifestPath + ".v1.bak"
	if existing, err := os.ReadFile(backupPath); err == nil && !bytesEqual(existing, backupData) {
		return emitMigrationFailure(stdout, planWithReason(plan, reasonManifestConflict), *jsonOutput, errors.New(reasonManifestConflict))
	}
	if _, err := os.Stat(backupPath); errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return createErr
		}
		_, writeErr := file.Write(backupData)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := registry.AtomicReplaceFile(manifestPath, manifestData, 0o644); err != nil {
		return err
	}
	plan.Status = "applied"
	emitMigrationPlan(stdout, plan, *jsonOutput)
	return nil
}

func runRebind(args []string, stdout, stderr io.Writer) error {
	projectSelector, remaining, err := takePath(args, "rebind")
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("rebind", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspacePath := flags.String("workspace", "", "new canonical workspace path")
	dryRun := flags.Bool("dry-run", false, "print the rebind plan without mutating")
	confirm := flags.Bool("confirm", false, "confirm the registry mutation")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(remaining); err != nil {
		return err
	}
	if flags.NArg() != 0 || *workspacePath == "" {
		return errors.New("rebind requires <project> and --workspace <path>")
	}
	plan, _, oldWorkspace, root, fingerprint, err := buildRebindPlan(projectSelector, *workspacePath)
	if err != nil {
		return emitMigrationFailure(stdout, planForFailure("rebind", projectSelector, err), *jsonOutput, err)
	}
	if plan.Status == "already_rebound" {
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if *dryRun {
		plan.Status = "planned"
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return nil
	}
	if !*confirm {
		plan.Status = "confirmation_required"
		plan.Reason = reasonConfirmationRequired
		emitMigrationPlan(stdout, plan, *jsonOutput)
		return errors.New("confirmation required; rerun with --confirm")
	}
	if err := revalidateMutationTarget(root, fingerprint); err != nil {
		return emitMigrationFailure(stdout, planWithReason(plan, reasonIdentityChangedAfterPlan), *jsonOutput, err)
	}
	store, err := registryStore()
	if err != nil {
		return err
	}
	current, err := store.Load()
	if err != nil {
		return err
	}
	currentOld, ok := findWorkspace(current, oldWorkspace.ID)
	if !ok || currentOld.PathKey != oldWorkspace.PathKey {
		return emitMigrationFailure(stdout, planWithReason(plan, reasonRebindAmbiguous), *jsonOutput, errors.New(reasonRebindAmbiguous))
	}
	for _, candidate := range current.Workspaces {
		if candidate.ID != oldWorkspace.ID && candidate.PathKey == workspace.PathKey(root) {
			return emitMigrationFailure(stdout, planWithReason(plan, reasonWorkspaceAlreadyBound), *jsonOutput, errors.New(reasonWorkspaceAlreadyBound))
		}
	}
	updated := workspaceRecord(oldWorkspace.ID, oldWorkspace.RepositoryID, oldWorkspace.Role, mustProbe(root))
	for index := range current.Workspaces {
		if current.Workspaces[index].ID == oldWorkspace.ID {
			current.Workspaces[index] = updated
		}
	}
	if err := store.Save(current); err != nil {
		return err
	}
	plan.Status = "applied"
	emitMigrationPlan(stdout, plan, *jsonOutput)
	return nil
}

func takePath(args []string, command string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("%s requires <path> before options", command)
	}
	return args[0], args[1:], nil
}

func buildAdoptPlan(path, version string) (migrationPlan, []byte, string, string, error) {
	root, probe, err := probeMigrationTarget(path)
	if err != nil {
		return planForFailure("adopt", path, err), nil, "", "", err
	}
	if probe.PrimaryRemote == nil || probe.Branch == "" || probe.Detached {
		err := errors.New(reasonRepositoryMismatch)
		return planForFailure("adopt", root, err), nil, root, probe.Fingerprint, err
	}
	manifestPath := filepath.Join(root, ".contextbridge", "manifest.json")
	if data, readErr := os.ReadFile(manifestPath); readErr == nil {
		var envelope manifestEnvelope
		if json.Unmarshal(data, &envelope) == nil && envelope.SchemaVersion == 2 {
			if _, parseErr := core.ParseManifest(data); parseErr == nil {
				return migrationPlan{Command: "adopt", Status: "already_adopted", Reason: reasonAlreadyAdopted, Target: root, Current: identityFromProbe(probe), Operations: []migrationOperation{}}, nil, root, probe.Fingerprint, nil
			}
		}
		err := errors.New(reasonManifestConflict)
		return planForFailure("adopt", root, err), nil, root, probe.Fingerprint, err
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return planForFailure("adopt", root, readErr), nil, root, probe.Fingerprint, readErr
	}
	projectID, repositoryID, err := resolveOrMintIDs(probe)
	if err != nil {
		return planForFailure("adopt", root, err), nil, root, probe.Fingerprint, err
	}
	manifest := core.Manifest{SchemaVersion: 2, Generator: core.Generator{Name: "contextbridge", Version: version}, Project: core.ManifestProject{ID: projectID, Name: filepath.Base(root), Repository: probe.PrimaryRemote.Key(), Profile: "core"}, Repository: &core.ManifestRepository{ID: repositoryID, Identity: *probe.PrimaryRemote, PrimaryRemoteName: probe.PrimaryRemoteName, CanonicalBranch: probe.Branch}, Lifecycle: core.Lifecycle{CreatedByContextBridge: true}}
	data, err := marshalManifest(manifest)
	if err != nil {
		return planForFailure("adopt", root, err), nil, root, probe.Fingerprint, err
	}
	plan := migrationPlan{Command: "adopt", Status: "planned", Reason: "ADOPT_EXISTING_REPOSITORY", Target: root, Current: identityFromProbe(probe), Proposed: identityFromProbe(probe), PreconditionFingerprint: probe.Fingerprint, RequiresConfirmation: true, Operations: []migrationOperation{{Kind: "create_directory", Path: ".contextbridge"}, {Kind: "write_file", Path: ".contextbridge/manifest.json", Size: len(data), SHA256: hashBytes(data)}}}
	plan.Proposed.ProjectID = projectID
	plan.Proposed.RepositoryID = repositoryID
	return plan, data, root, probe.Fingerprint, nil
}

func buildUpgradePlan(path, version string) (migrationPlan, []byte, []byte, string, string, error) {
	root, probe, err := probeMigrationTarget(path)
	if err != nil {
		return planForFailure("upgrade", path, err), nil, nil, "", "", err
	}
	manifestPath := filepath.Join(root, ".contextbridge", "manifest.json")
	oldData, err := os.ReadFile(manifestPath)
	if err != nil {
		return planForFailure("upgrade", root, errors.New(reasonManifestConflict)), nil, nil, root, probe.Fingerprint, errors.New(reasonManifestConflict)
	}
	var envelope manifestEnvelope
	if err := json.Unmarshal(oldData, &envelope); err != nil {
		return planForFailure("upgrade", root, errors.New(reasonManifestConflict)), nil, nil, root, probe.Fingerprint, errors.New(reasonManifestConflict)
	}
	if envelope.SchemaVersion > 2 {
		err := errors.New(reasonUnknownNewerManifest)
		return planForFailure("upgrade", root, err), nil, nil, root, probe.Fingerprint, err
	}
	if envelope.SchemaVersion == 2 {
		if _, err := core.ParseManifest(oldData); err != nil {
			return planForFailure("upgrade", root, errors.New(reasonManifestConflict)), nil, nil, root, probe.Fingerprint, errors.New(reasonManifestConflict)
		}
		return migrationPlan{Command: "upgrade", Status: "already_upgraded", Reason: reasonAlreadyUpgraded, Target: root, Current: identityFromProbe(probe), Operations: []migrationOperation{}}, nil, nil, root, probe.Fingerprint, nil
	}
	v1, err := core.ParseManifest(oldData)
	if err != nil || envelope.SchemaVersion != 1 {
		err = errors.New(reasonManifestConflict)
		return planForFailure("upgrade", root, err), nil, nil, root, probe.Fingerprint, err
	}
	identity := core.RemoteIdentity{}
	primaryName := ""
	if !v1.Lifecycle.LocalOnly {
		if probe.PrimaryRemote == nil || probe.Branch == "" {
			err := errors.New(reasonRepositoryMismatch)
			return planForFailure("upgrade", root, err), nil, nil, root, probe.Fingerprint, err
		}
		identity = *probe.PrimaryRemote
		primaryName = probe.PrimaryRemoteName
		if v1.Project.Repository != "" {
			expected, normalizeErr := core.NormalizeRemote("https://github.com/" + strings.TrimSuffix(v1.Project.Repository, ".git"))
			if normalizeErr != nil || !expected.Equal(identity) {
				err := errors.New(reasonRepositoryMismatch)
				return planForFailure("upgrade", root, err), nil, nil, root, probe.Fingerprint, err
			}
		}
	}
	projectID, repositoryID, err := resolveOrMintIDs(probe)
	if err != nil {
		return planForFailure("upgrade", root, err), nil, nil, root, probe.Fingerprint, err
	}
	if v1.Generator.Version == "" {
		v1.Generator.Version = version
	}
	if probe.Branch == "" {
		err := errors.New(reasonRepositoryMismatch)
		return planForFailure("upgrade", root, err), nil, nil, root, probe.Fingerprint, err
	}
	v2 := core.Manifest{SchemaVersion: 2, Generator: v1.Generator, Project: core.ManifestProject{ID: projectID, Name: v1.Project.Name, Repository: v1.Project.Repository, Profile: v1.Project.Profile}, Lifecycle: v1.Lifecycle, Files: v1.Files, Repository: &core.ManifestRepository{ID: repositoryID, Identity: identity, PrimaryRemoteName: primaryName, CanonicalBranch: probe.Branch}}
	data, err := marshalManifest(v2)
	if err != nil {
		return planForFailure("upgrade", root, err), nil, nil, root, probe.Fingerprint, err
	}
	plan := migrationPlan{Command: "upgrade", Status: "planned", Reason: "UPGRADE_MANIFEST_V1_TO_V2", Target: root, Current: identityFromProbe(probe), Proposed: identityFromProbe(probe), PreconditionFingerprint: probe.Fingerprint, RequiresConfirmation: true, Operations: []migrationOperation{{Kind: "preserve_backup", Path: ".contextbridge/manifest.json.v1.bak", Size: len(oldData), SHA256: hashBytes(oldData)}, {Kind: "replace_file", Path: ".contextbridge/manifest.json", Size: len(data), SHA256: hashBytes(data)}}}
	plan.Proposed.ProjectID = projectID
	plan.Proposed.RepositoryID = repositoryID
	return plan, data, oldData, root, probe.Fingerprint, nil
}

func buildRebindPlan(selector, target string) (migrationPlan, core.Registry, core.WorkspaceRecord, string, string, error) {
	store, err := registryStore()
	if err != nil {
		return planForFailure("rebind", selector, err), core.Registry{}, core.WorkspaceRecord{}, "", "", err
	}
	value, err := store.Load()
	if err != nil {
		return planForFailure("rebind", selector, err), core.Registry{}, core.WorkspaceRecord{}, "", "", err
	}
	project, err := resolveHandoffProject(value, selector)
	if err != nil {
		if strings.Contains(err.Error(), reasonProjectNotFound) {
			err = errors.New(reasonProjectNotFound)
		} else {
			err = errors.New(reasonProjectAmbiguous)
		}
		return planForFailure("rebind", selector, err), value, core.WorkspaceRecord{}, "", "", err
	}
	repository, err := repositoryForProject(value, project.ID)
	if err != nil {
		return planForFailure("rebind", selector, errors.New(reasonRepositoryMismatch)), value, core.WorkspaceRecord{}, "", "", errors.New(reasonRepositoryMismatch)
	}
	old, ok := canonicalWorkspace(value, repository.ID)
	if !ok {
		err := errors.New(reasonWorkspaceNotFound)
		return planForFailure("rebind", selector, err), value, core.WorkspaceRecord{}, "", "", err
	}
	root, probe, err := probeMigrationTarget(target)
	if err != nil {
		err = errors.New(reasonTargetNotGit)
		return planForFailure("rebind", selector, err), value, old, "", "", err
	}
	if root == old.Path && probe.PathKey == old.PathKey {
		return migrationPlan{Command: "rebind", Status: "already_rebound", Reason: "ALREADY_REBOUND", Target: root, Current: identityFromWorkspace(old), Proposed: identityFromProbe(probe), Operations: []migrationOperation{}}, value, old, root, probe.Fingerprint, nil
	}
	if _, statErr := os.Stat(old.Path); statErr == nil {
		err := errors.New(reasonRebindAmbiguous)
		return planForFailure("rebind", selector, err), value, old, root, probe.Fingerprint, err
	}
	if probe.PrimaryRemote == nil || !probe.PrimaryRemote.Equal(repository.Identity) || probe.Detached || probe.Branch != repository.CanonicalBranch {
		err := errors.New(reasonRebindIdentityMismatch)
		return planForFailure("rebind", selector, err), value, old, root, probe.Fingerprint, err
	}
	for _, candidate := range value.Workspaces {
		if candidate.ID != old.ID && candidate.PathKey == probe.PathKey {
			err := errors.New(reasonWorkspaceAlreadyBound)
			return planForFailure("rebind", selector, err), value, old, root, probe.Fingerprint, err
		}
	}
	plan := migrationPlan{Command: "rebind", Status: "planned", Reason: "REBIND_CANONICAL_WORKSPACE", Target: root, Current: identityFromWorkspace(old), Proposed: identityFromProbe(probe), PreconditionFingerprint: probe.Fingerprint, RequiresConfirmation: true, Operations: []migrationOperation{{Kind: "update_registry_workspace", Path: old.ID}}}
	return plan, value, old, root, probe.Fingerprint, nil
}

func probeMigrationTarget(path string) (string, core.WorkspaceProbe, error) {
	if _, statErr := os.Stat(path); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return "", core.WorkspaceProbe{}, errors.New(reasonTargetNotFound)
		}
		return "", core.WorkspaceProbe{}, errors.New(reasonGitProbeFailed)
	}
	canonical, err := workspace.CanonicalPath(path)
	if err != nil {
		return "", core.WorkspaceProbe{}, errors.New(reasonGitProbeFailed)
	}
	probe, err := (workspace.Prober{}).Probe(canonical)
	if err != nil {
		return "", core.WorkspaceProbe{}, errors.New(reasonGitProbeFailed)
	}
	if probe.Topology == core.TopologyNonGit || probe.GitRoot == "" {
		if _, gitErr := os.Stat(filepath.Join(canonical, ".git")); gitErr == nil {
			return "", core.WorkspaceProbe{}, errors.New(reasonGitRepositoryInaccessible)
		}
		return "", core.WorkspaceProbe{}, errors.New(reasonTargetNotGit)
	}
	return probe.GitRoot, probe, nil
}

func revalidateRepository(root, fingerprint string) error {
	probe, err := (workspace.Prober{}).Probe(root)
	if err != nil || probe.Fingerprint != fingerprint {
		return errors.New(reasonIdentityChangedAfterPlan)
	}
	return nil
}

func revalidateMutationTarget(root, fingerprint string) error {
	if err := revalidateRepository(root, fingerprint); err != nil {
		return err
	}
	probe, err := (workspace.Prober{}).Probe(root)
	if err != nil {
		return errors.New(reasonIdentityChangedAfterPlan)
	}
	store, err := registryStore()
	if err != nil {
		return err
	}
	value, err := store.Load()
	if err != nil {
		return err
	}
	for _, registered := range value.Workspaces {
		if registered.PathKey != probe.PathKey {
			continue
		}
		var repository core.RepositoryRecord
		for _, candidate := range value.Repositories {
			if candidate.ID == registered.RepositoryID {
				repository = candidate
				break
			}
		}
		project, projectErr := projectForRepository(value, registered.RepositoryID)
		if projectErr != nil || repository.ID == "" {
			return errors.New(reasonRepositoryMismatch)
		}
		decision := core.EvaluateGuard(guardExpected(project, repository, registered, ""), core.GuardActual{ProjectFound: true, ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, Probe: probe})
		if !decision.Allowed {
			return core.WrongWorkspaceError{Reasons: decision.Reasons}
		}
		break
	}
	return nil
}

func resolveOrMintIDs(probe core.WorkspaceProbe) (string, string, error) {
	store, err := registryStore()
	if err == nil {
		value, loadErr := store.Load()
		if loadErr == nil && probe.PrimaryRemote != nil {
			for _, repository := range value.Repositories {
				if repository.Identity.Equal(*probe.PrimaryRemote) {
					return repository.ProjectID, repository.ID, nil
				}
			}
		}
	}
	projectID, err := idgen.New(core.ProjectIDPrefix)
	if err != nil {
		return "", "", err
	}
	repositoryID, err := idgen.New(core.RepositoryIDPrefix)
	if err != nil {
		return "", "", err
	}
	return projectID, repositoryID, nil
}

func marshalManifest(manifest core.Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func identityFromProbe(probe core.WorkspaceProbe) migrationIdentity {
	identity := migrationIdentity{Path: probe.CanonicalPath, GitRoot: probe.GitRoot, GitDir: probe.GitDir, GitCommonDir: probe.GitCommonDir, Branch: probe.Branch, Head: probe.Head, Fingerprint: probe.Fingerprint}
	if probe.PrimaryRemote != nil {
		identity.RepositoryIdentity = *probe.PrimaryRemote
	}
	return identity
}

func identityFromWorkspace(record core.WorkspaceRecord) migrationIdentity {
	return migrationIdentity{WorkspaceID: record.ID, Path: record.Path, GitRoot: record.GitRoot, GitDir: record.GitDir, GitCommonDir: record.GitCommonDir}
}

func mustProbe(path string) core.WorkspaceProbe {
	probe, _ := (workspace.Prober{}).Probe(path)
	return probe
}

func findWorkspace(value core.Registry, id string) (core.WorkspaceRecord, bool) {
	for _, record := range value.Workspaces {
		if record.ID == id {
			return record, true
		}
	}
	return core.WorkspaceRecord{}, false
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func bytesEqual(left, right []byte) bool { return string(left) == string(right) }

func planForFailure(command, target string, err error) migrationPlan {
	return migrationPlan{Command: command, Status: "failed", Reason: err.Error(), Target: target, Operations: []migrationOperation{}}
}

func planWithReason(plan migrationPlan, reason string) migrationPlan {
	plan.Status = "failed"
	plan.Reason = reason
	return plan
}

func emitMigrationFailure(stdout io.Writer, plan migrationPlan, jsonOutput bool, err error) error {
	emitMigrationPlan(stdout, plan, jsonOutput)
	return err
}

func emitMigrationPlan(stdout io.Writer, plan migrationPlan, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.Marshal(plan)
		fmt.Fprintln(stdout, string(data))
		return
	}
	label := "READY"
	if plan.Status == "failed" {
		label = "BLOCKED"
	}
	if plan.Status == "planned" || plan.Status == "confirmation_required" {
		label = "WARNING"
	}
	fmt.Fprintf(stdout, "%s: %s\nTarget: %s\nReason: %s\n", strings.Title(plan.Command), label, plan.Target, plan.Reason)
	if plan.RequiresConfirmation {
		fmt.Fprintln(stdout, "Action: review the plan and rerun with --confirm if approved.")
	}
	if plan.PreconditionFingerprint != "" {
		fmt.Fprintf(stdout, "Precondition: captured\n")
	}
	for _, operation := range plan.Operations {
		fmt.Fprintf(stdout, "Plan: %s %s\n", operation.Kind, operation.Path)
	}
	fmt.Fprintf(stdout, "\n%s\n", strings.ToUpper(plan.Status))
}
