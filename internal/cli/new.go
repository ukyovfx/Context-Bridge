package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/localprofile"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
)

func runNew(args []string, stdout, stderr io.Writer, version string) error {
	project, remaining, err := takeProject(args)
	if err != nil {
		return errors.New(strings.Replace(err.Error(), "init", "new", 1))
	}
	flags := flag.NewFlagSet("new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profile := flags.String("profile", "core", "generated profile: core or openai")
	github := flags.Bool("github", false, "also create and push a private GitHub repository")
	owner := flags.String("owner", os.Getenv("CONTEXTBRIDGE_GITHUB_OWNER"), "personal GitHub login (with --github)")
	dryRun := flags.Bool("dry-run", false, "print the plan without applying it")
	if err := flags.Parse(remaining); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}

	home, err := registryDefaultHome()
	if err != nil {
		return err
	}
	configured, err := localprofile.Load(home)
	if errors.Is(err, localprofile.ErrNotConfigured) {
		return errors.New("workspace profile is not configured; run contextbridge setup first")
	}
	if err != nil {
		return fmt.Errorf("workspace profile is invalid; run contextbridge setup: %w", err)
	}
	profilePlan, err := localprofile.Plan(configured.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("workspace profile is invalid; run contextbridge setup: %w", err)
	}
	if len(profilePlan.Create) != 0 {
		return errors.New("workspace profile is incomplete; run contextbridge setup first")
	}
	target := filepath.Join(profilePlan.Layout.ActiveRoot, project)
	if err := preflightNewRegistry(project); err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprintf(stdout, "new preview: target %s\n", target)
		fmt.Fprintln(stdout, "new preview: register project, repository, and canonical workspace after apply")
	}

	initArgs := []string{project, "--root", profilePlan.Layout.ActiveRoot, "--profile", *profile}
	if *owner != "" {
		initArgs = append(initArgs, "--owner", *owner)
	}
	if !*github {
		initArgs = append(initArgs, "--local-only")
	}
	if *dryRun {
		initArgs = append(initArgs, "--dry-run")
	}
	if err := runInitWithRegistration(initArgs, stdout, stderr, version, true); err != nil {
		return err
	}
	if !*dryRun {
		fmt.Fprintf(stdout, "next: open %s as a Codex Local Project\n", target)
	}
	return nil
}

func registryDefaultHome() (string, error) {
	return registry.DefaultHome()
}

func preflightNewRegistry(project string) error {
	store, err := registryStore()
	if err != nil {
		return err
	}
	value, err := store.Load()
	if err != nil {
		return err
	}
	for _, existing := range value.Projects {
		if existing.DisplayName == project || contains(existing.Aliases, project) {
			return fmt.Errorf("identity conflict: project %q is already registered", project)
		}
	}
	return nil
}
