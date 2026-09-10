package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/apply"
	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/idgen"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func runInit(args []string, stdout, stderr io.Writer, version string) error {
	project, remaining, err := takeProject(args)
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rootDefault := os.Getenv("CONTEXTBRIDGE_PROJECTS_ROOT")
	if rootDefault == "" {
		rootDefault, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("current directory: %w", err)
		}
	}
	root := flags.String("root", rootDefault, "existing projects root")
	owner := flags.String("owner", os.Getenv("CONTEXTBRIDGE_GITHUB_OWNER"), "personal GitHub login")
	profile := flags.String("profile", "core", "generated profile: core or openai")
	localOnly := flags.Bool("local-only", false, "create and commit locally without a GitHub remote")
	dryRun := flags.Bool("dry-run", false, "print the plan without applying it")
	if err := flags.Parse(remaining); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("projects root: %w", err)
	}
	var remoteIdentity *core.RemoteIdentity
	if !*localOnly {
		identity, identityErr := readGitHubIdentity()
		if identityErr != nil {
			return identityErr
		}
		if *owner == "" {
			*owner = identity.Login
		}
		if !strings.EqualFold(*owner, identity.Login) {
			return errors.New("safety abort: owner must match the authenticated personal GitHub account; organizations are not supported")
		}
		if err := requireRepositoryAbsent(*owner, project); err != nil {
			return err
		}
		normalized, err := core.NormalizeRemote("https://github.com/" + *owner + "/" + project + ".git")
		if err != nil {
			return err
		}
		remoteIdentity = &normalized
	}
	projectID, err := idgen.New(core.ProjectIDPrefix)
	if err != nil {
		return err
	}
	repositoryID, err := idgen.New(core.RepositoryIDPrefix)
	if err != nil {
		return err
	}
	snapshot, err := inspectInit(absRoot, project)
	if err != nil {
		return err
	}
	primaryRemote := ""
	if !*localOnly {
		primaryRemote = "origin"
	}
	plan, err := core.BuildInitPlan(core.InitRequest{
		Name: project, Root: absRoot, Owner: *owner, Profile: *profile,
		LocalOnly: *localOnly, GeneratorVersion: version, ProjectID: projectID,
		RepositoryID: repositoryID, RepositoryIdentity: remoteIdentity,
		PrimaryRemoteName: primaryRemote, CanonicalBranch: "main",
	}, snapshot)
	if err != nil {
		return err
	}
	if *dryRun {
		data, err := plan.JSON()
		if err != nil {
			return err
		}
		if _, err := stdout.Write(data); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "dry-run: zero mutations performed")
		return nil
	}
	applier := apply.Applier{Runner: apply.CommandRunner{Stdout: stdout, Stderr: stderr}}
	if err := applier.Apply(plan); err != nil {
		return err
	}
	if !*localOnly {
		probe, err := (workspace.Prober{}).Probe(plan.Target())
		if err != nil {
			return fmt.Errorf("project created but workspace registration probe failed: %w", err)
		}
		store, err := registryStore()
		if err != nil {
			return err
		}
		registryValue, err := store.Load()
		if err != nil {
			return err
		}
		workspaceID, err := idgen.New(core.WorkspaceIDPrefix)
		if err != nil {
			return err
		}
		registryValue.Projects = append(registryValue.Projects, core.ProjectRecord{ID: projectID, DisplayName: project})
		registryValue.Repositories = append(registryValue.Repositories, core.RepositoryRecord{ID: repositoryID, ProjectID: projectID, Identity: *remoteIdentity, PrimaryRemoteName: "origin", CanonicalBranch: "main"})
		registryValue.Workspaces = append(registryValue.Workspaces, workspaceRecord(workspaceID, repositoryID, core.RoleCanonical, probe))
		if err := store.Save(registryValue); err != nil {
			return fmt.Errorf("project created but registry write failed: %w", err)
		}
	}
	fmt.Fprintf(stdout, "initialized %s\n", plan.Target())
	return nil
}

func takeProject(args []string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, errors.New("init requires <project> before options")
	}
	return args[0], args[1:], nil
}

func inspectInit(root, project string) (core.InitSnapshot, error) {
	exists, isDir, isRoot, reparse, err := safety.InspectRoot(root)
	if err != nil {
		return core.InitSnapshot{}, err
	}
	targetExists, err := safety.TargetExists(filepath.Join(root, project))
	if err != nil {
		return core.InitSnapshot{}, err
	}
	insideGit, err := safety.InsideGitRepository(root)
	if err != nil {
		return core.InitSnapshot{}, err
	}
	return core.InitSnapshot{RootExists: exists, RootIsDirectory: isDir, RootIsFilesystemRoot: isRoot, RootHasReparsePoint: reparse, TargetExists: targetExists, InsideGitRepository: insideGit}, nil
}

func workspaceRecord(id, repositoryID string, role core.RegistryRole, probe core.WorkspaceProbe) core.WorkspaceRecord {
	return core.WorkspaceRecord{ID: id, RepositoryID: repositoryID, Path: probe.CanonicalPath, PathKey: probe.PathKey, GitRoot: probe.GitRoot, GitRootKey: probe.GitRootKey, GitDir: probe.GitDir, GitDirKey: probe.GitDirKey, GitCommonDir: probe.GitCommonDir, GitCommonDirKey: probe.GitCommonDirKey, Topology: probe.Topology, Role: role}
}
