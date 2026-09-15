package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/codexbootstrap"
	"github.com/ukyovfx/Context-Bridge/internal/localprofile"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
)

var setupInput io.Reader = os.Stdin
var setupGitHubIdentity = readGitHubIdentity

func runSetup(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "perform read-only checks only")
	workspaceRoot := flags.String("workspace-root", "", "absolute local development workspace root")
	confirm := flags.Bool("confirm", false, "approve the exact machine onboarding preview")
	codexBootstrap := flags.Bool("codex-bootstrap", false, "add the managed Context Bridge block to the Codex global AGENTS.md")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("setup accepts no positional arguments")
	}
	if _, err := lookPath("git"); err != nil {
		return err
	}
	if _, err := runOutput("", "git", "--version"); err != nil {
		return err
	}
	if _, err := runOutput("", "git", "config", "--get", "user.name"); err != nil {
		return errors.New("Git user.name is not configured")
	}
	if _, err := runOutput("", "git", "config", "--get", "user.email"); err != nil {
		return errors.New("Git user.email is not configured")
	}
	identity, err := setupGitHubIdentity()
	if err != nil {
		return err
	}
	localHome, err := registry.DefaultHome()
	if err != nil {
		return err
	}
	profile, profileErr := localprofile.Load(localHome)
	profileMissing := errors.Is(profileErr, localprofile.ErrNotConfigured)
	if profileErr != nil && !profileMissing {
		return errors.New("LOCAL_PROFILE_INVALID")
	}
	if profileMissing {
		root, chooseErr := chooseWorkspaceRoot(*workspaceRoot, stdout)
		if chooseErr != nil {
			return chooseErr
		}
		profilePlan, planErr := localprofile.Plan(root)
		if planErr != nil {
			return planErr
		}
		profile = profilePlan.Profile
	} else if *workspaceRoot != "" && filepath.Clean(*workspaceRoot) != profile.WorkspaceRoot {
		return errors.New("workspace profile already exists; remove the conflicting workspace-root option")
	}
	profilePlan, err := localprofile.Plan(profile.WorkspaceRoot)
	if err != nil {
		return err
	}
	var codexPlan codexbootstrap.Plan
	if *codexBootstrap {
		codexPlan, err = codexbootstrap.PlanBootstrap()
		if err != nil {
			return err
		}
		if codexPlan.Action == "blocked" {
			emitSetupPreview(stdout, localHome, profile, profileMissing, profilePlan, &codexPlan)
			return errors.New("Codex bootstrap is blocked by an existing global instruction conflict")
		}
	}
	fmt.Fprintf(stdout, "setup ok: git configured; GitHub personal account %s authenticated; no credentials stored\n", identity.Login)
	emitSetupPreview(stdout, localHome, profile, profileMissing, profilePlan, optionalCodexPlan(*codexBootstrap, codexPlan))
	changes := profileMissing || len(profilePlan.Create) > 0 || (*codexBootstrap && codexPlan.Action != "none")
	if !changes {
		fmt.Fprintln(stdout, "setup: no machine changes needed")
		return nil
	}
	if *dryRun {
		fmt.Fprintln(stdout, "dry-run: setup is read-only; zero mutations performed")
		return nil
	}
	if !*confirm {
		approved, approvalErr := confirmWorkspacePlan(stdout)
		if approvalErr != nil {
			return approvalErr
		}
		if !approved {
			return errors.New("setup declined; no changes made")
		}
	}
	created, createErr := localprofile.Create(profilePlan)
	if createErr != nil {
		emitCreatedDirectories(stdout, created)
		return createErr
	}
	if profileMissing {
		if saveErr := localprofile.Save(localHome, profile); saveErr != nil {
			emitCreatedDirectories(stdout, created)
			return fmt.Errorf("workspace directories created but profile was not saved: %w", saveErr)
		}
		fmt.Fprintln(stdout, "workspace profile saved")
	}
	emitCreatedDirectories(stdout, created)
	if *codexBootstrap && codexPlan.Action != "none" {
		if err := codexbootstrap.Apply(codexPlan); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Codex global router installed")
	}
	return nil
}

func optionalCodexPlan(enabled bool, plan codexbootstrap.Plan) *codexbootstrap.Plan {
	if !enabled {
		return nil
	}
	return &plan
}

func emitSetupPreview(stdout io.Writer, localHome string, profile localprofile.Profile, profileMissing bool, profilePlan localprofile.LayoutPlan, codexPlan *codexbootstrap.Plan) {
	fmt.Fprintln(stdout, "setup preview (exact machine changes):")
	profileAction := "no change"
	if profileMissing {
		profileAction = "create"
	} else if len(profilePlan.Create) > 0 {
		profileAction = "create missing directories"
	}
	fmt.Fprintf(stdout, "  local profile: %s (%s)\n", filepath.Join(localHome, localprofile.Filename), profileAction)
	emitWorkspacePlan(stdout, profilePlan)
	if codexPlan == nil {
		fmt.Fprintln(stdout, "  Codex global router: not requested")
		return
	}
	fmt.Fprintf(stdout, "  Codex global AGENTS.md: %s (%s)\n", codexPlan.Path, codexPlan.Action)
	if len(codexPlan.Warnings) > 0 {
		for _, warning := range codexPlan.Warnings {
			fmt.Fprintf(stdout, "    warning: %s\n", warning)
		}
	}
	if codexPlan.Action != "none" {
		fmt.Fprintln(stdout, "    managed block:")
		for _, line := range strings.Split(strings.TrimSuffix(codexbootstrap.ManagedBlock(), "\n"), "\n") {
			fmt.Fprintf(stdout, "      %s\n", line)
		}
	}
}

func chooseWorkspaceRoot(explicit string, stdout io.Writer) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	suggested := localprofile.SuggestedRoot()
	if !setupInteractive() {
		return "", fmt.Errorf("workspace root is required in non-interactive setup; suggested root: %s", suggested)
	}
	fmt.Fprintf(stdout, "Workspace root [%s]: ", suggested)
	line, err := bufio.NewReader(setupInput).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", errors.New("workspace root selection was cancelled")
	}
	if strings.TrimSpace(line) == "" {
		return suggested, nil
	}
	return strings.TrimSpace(line), nil
}

func setupInteractive() bool {
	if setupInput != os.Stdin {
		return true
	}
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func emitWorkspacePlan(stdout io.Writer, plan localprofile.LayoutPlan) {
	rootState := "exists"
	for _, created := range plan.Create {
		if created == plan.Layout.WorkspaceRoot {
			rootState = "create"
			break
		}
	}
	fmt.Fprintf(stdout, "workspace root: %s (%s)\n", plan.Layout.WorkspaceRoot, rootState)
	for _, entry := range []struct {
		name string
		path string
	}{
		{"active", plan.Layout.ActiveRoot},
		{"worktrees", plan.Layout.WorktreeRoot},
		{"archive", plan.Layout.ArchiveRoot},
		{"private", plan.Layout.PrivateRoot},
	} {
		state := "exists"
		for _, created := range plan.Create {
			if created == entry.path {
				state = "create"
				break
			}
		}
		fmt.Fprintf(stdout, "  %s: %s (%s)\n", entry.name, entry.path, state)
	}
}

func confirmWorkspacePlan(stdout io.Writer) (bool, error) {
	if !setupInteractive() {
		return false, errors.New("explicit approval required; rerun setup with --confirm")
	}
	fmt.Fprint(stdout, "Apply the exact setup preview? [y/N]: ")
	line, err := bufio.NewReader(setupInput).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false, errors.New("workspace setup approval was cancelled")
	}
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}

func emitCreatedDirectories(stdout io.Writer, created []string) {
	if len(created) == 0 {
		return
	}
	fmt.Fprintln(stdout, "workspace directories created:")
	for _, path := range created {
		fmt.Fprintf(stdout, "  %s\n", path)
	}
}
