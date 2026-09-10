package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/idgen"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func runRegistry(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("registry requires list, show, or register")
	}
	switch args[0] {
	case "list":
		return runRegistryList(args[1:], stdout)
	case "show":
		return runRegistryShow(args[1:], stdout)
	case "register":
		return runRegistryRegister(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown registry command %q", args[0])
	}
}

func runRegistryList(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("registry list accepts no arguments")
	}
	store, err := registryStore()
	if err != nil {
		return err
	}
	value, err := store.Load()
	if err != nil {
		return err
	}
	for _, project := range value.Sorted().Projects {
		fmt.Fprintf(stdout, "%s\t%s\n", project.ID, project.DisplayName)
	}
	return nil
}

func runRegistryShow(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("registry show requires <project>")
	}
	store, err := registryStore()
	if err != nil {
		return err
	}
	value, err := store.Load()
	if err != nil {
		return err
	}
	project, err := resolveProject(value, args[0])
	if err != nil {
		return err
	}
	repository, err := repositoryForProject(value, project.ID)
	if err != nil {
		return err
	}
	workspaces := make([]core.WorkspaceRecord, 0)
	for _, registered := range value.Workspaces {
		if registered.RepositoryID == repository.ID {
			workspaces = append(workspaces, registered)
		}
	}
	result := struct {
		Project    core.ProjectRecord     `json:"project"`
		Repository core.RepositoryRecord  `json:"repository"`
		Workspaces []core.WorkspaceRecord `json:"workspaces"`
	}{project, repository, workspaces}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", data)
	return err
}

func runRegistryRegister(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("registry register requires <path> before options")
	}
	path := args[0]
	flags := flag.NewFlagSet("registry register", flag.ContinueOnError)
	flags.SetOutput(stderr)
	roleName := flags.String("role", "canonical", "canonical or alternate")
	dryRun := flags.Bool("dry-run", false, "print registration without writing the registry")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected registry register arguments")
	}
	role := core.RoleCanonical
	if *roleName == "alternate" {
		role = core.RoleRegisteredAlternate
	} else if *roleName != "canonical" {
		return errors.New("role must be canonical or alternate")
	}
	probe, err := (workspace.Prober{}).Probe(path)
	if err != nil {
		return err
	}
	if probe.Topology == core.TopologyNonGit || probe.PrimaryRemote == nil || probe.Branch == "" || probe.Detached || probe.PathKey != probe.GitRootKey {
		return errors.New("registration requires a branch-attached Git workspace with an unambiguous origin")
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
		if registered.PathKey == probe.PathKey {
			return errors.New("workspace is already registered")
		}
	}

	displayName := filepath.Base(probe.GitRoot)
	projectID := ""
	repositoryID := ""
	for _, repository := range value.Repositories {
		if repository.Identity.Equal(*probe.PrimaryRemote) {
			projectID = repository.ProjectID
			repositoryID = repository.ID
			if role == core.RoleCanonical {
				for _, registered := range value.Workspaces {
					if registered.RepositoryID == repository.ID && registered.Role == core.RoleCanonical {
						return errors.New("repository already has a canonical workspace; register this path as alternate")
					}
				}
			}
			break
		}
	}
	if projectID == "" {
		if manifest, manifestErr := readManifest(probe.GitRoot); manifestErr == nil {
			displayName = manifest.Project.Name
			if manifest.SchemaVersion == 2 {
				projectID = manifest.Project.ID
				repositoryID = manifest.Repository.ID
				if !manifest.Repository.Identity.Equal(*probe.PrimaryRemote) {
					return errors.New("manifest repository identity does not match the workspace origin")
				}
			}
		}
		if projectID == "" {
			projectID, err = idgen.New(core.ProjectIDPrefix)
			if err != nil {
				return err
			}
			repositoryID, err = idgen.New(core.RepositoryIDPrefix)
			if err != nil {
				return err
			}
		}
		value.Projects = append(value.Projects, core.ProjectRecord{ID: projectID, DisplayName: displayName})
		value.Repositories = append(value.Repositories, core.RepositoryRecord{ID: repositoryID, ProjectID: projectID, Identity: *probe.PrimaryRemote, PrimaryRemoteName: probe.PrimaryRemoteName, CanonicalBranch: probe.Branch})
	}
	workspaceID, err := idgen.New(core.WorkspaceIDPrefix)
	if err != nil {
		return err
	}
	record := workspaceRecord(workspaceID, repositoryID, role, probe)
	value.Workspaces = append(value.Workspaces, record)
	if err := value.Validate(); err != nil {
		return err
	}
	if *dryRun {
		data, _ := json.MarshalIndent(record, "", "  ")
		fmt.Fprintf(stdout, "%s\ndry-run: zero registry and repository mutations performed\n", data)
		return nil
	}
	if err := store.Save(value); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "registered %s as %s\n", probe.CanonicalPath, role)
	return nil
}
