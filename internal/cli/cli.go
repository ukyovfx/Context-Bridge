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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/apply"
	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

const productLine = "Context Bridge — GPT ↔ Codex Development Context Bridge"

func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "setup":
		err = runSetup(args[1:], stdout, stderr)
	case "init":
		err = runInit(args[1:], stdout, stderr, version)
	case "doctor":
		err = runDoctor(args[1:], stdout, stderr)
	case "version":
		if len(args) != 1 {
			err = errors.New("version accepts no arguments")
		} else {
			fmt.Fprintf(stdout, "contextbridge %s\n", version)
		}
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		fmt.Fprintf(stderr, "contextbridge: %v\n", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, productLine)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  contextbridge setup [--dry-run]")
	fmt.Fprintln(w, "  contextbridge init <project> [--root PATH] [--owner USER] [--profile core|openai] [--local-only] [--dry-run]")
	fmt.Fprintln(w, "  contextbridge doctor [--project PATH]")
	fmt.Fprintln(w, "  contextbridge version")
}

func runSetup(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "perform read-only checks only")
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
	identity, err := readGitHubIdentity()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "setup ok: git configured; GitHub personal account %s authenticated; no credentials stored\n", identity.Login)
	if *dryRun {
		fmt.Fprintln(stdout, "dry-run: setup is read-only; zero mutations performed")
	}
	return nil
}

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
	}
	snapshot, err := inspectInit(absRoot, project)
	if err != nil {
		return err
	}
	plan, err := core.BuildInitPlan(core.InitRequest{
		Name: project, Root: absRoot, Owner: *owner, Profile: *profile,
		LocalOnly: *localOnly, GeneratorVersion: version,
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
	return core.InitSnapshot{
		RootExists: exists, RootIsDirectory: isDir, RootIsFilesystemRoot: isRoot,
		RootHasReparsePoint: reparse, TargetExists: targetExists, InsideGitRepository: insideGit,
	}, nil
}

type githubIdentity struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func readGitHubIdentity() (githubIdentity, error) {
	if _, err := lookPath("gh"); err != nil {
		return githubIdentity{}, err
	}
	data, err := runOutput("", "gh", "api", "user")
	if err != nil {
		return githubIdentity{}, errors.New("GitHub CLI is not authenticated or GitHub is unavailable")
	}
	var identity githubIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return githubIdentity{}, errors.New("GitHub identity response was invalid")
	}
	if identity.Login == "" || identity.Type != "User" {
		return githubIdentity{}, errors.New("safety abort: authenticated GitHub identity is not a personal user account")
	}
	return identity, nil
}

func requireRepositoryAbsent(owner, project string) error {
	cmd := exec.Command("gh", "api", "repos/"+owner+"/"+project)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return errors.New("safety abort: GitHub repository already exists")
	}
	message := string(output)
	if strings.Contains(message, "HTTP 404") || strings.Contains(message, `"status":"404"`) || strings.Contains(message, `"status": "404"`) {
		return nil
	}
	return errors.New("could not prove that the target GitHub repository is absent")
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	project := flags.String("project", "", "Context Bridge project path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("doctor accepts no positional arguments")
	}
	if *project == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		*project = cwd
	}
	absProject, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(absProject, ".contextbridge", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return errors.New("not a Context Bridge-created project: manifest missing or unreadable")
	}
	var manifest core.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return errors.New("manifest is invalid")
	}
	if manifest.SchemaVersion != 1 || manifest.Generator.Name != "contextbridge" || !manifest.Lifecycle.CreatedByContextBridge {
		return errors.New("manifest is not a supported Context Bridge V1 manifest")
	}
	for _, item := range manifest.Files {
		path, err := safety.JoinWithin(absProject, item.Path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("managed file missing: %s", item.Path)
		}
		sum := sha256.Sum256(contents)
		actual := hex.EncodeToString(sum[:])
		if actual != item.SHA256 {
			if filepath.ToSlash(item.Path) == "docs/agent/CURRENT-STATE.md" {
				return errors.New("stale CURRENT-STATE detected: hash differs from manifest")
			}
			return fmt.Errorf("managed file changed: %s", item.Path)
		}
	}
	gitRoot, err := runOutput(absProject, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("project is not a Git repository")
	}
	resolvedGitRoot, err := filepath.Abs(strings.TrimSpace(string(gitRoot)))
	if err != nil || !samePath(resolvedGitRoot, absProject) {
		return errors.New("Git root does not match the manifest project root")
	}
	if !manifest.Lifecycle.LocalOnly {
		origin, err := runOutput(absProject, "git", "remote", "get-url", "origin")
		if err != nil {
			return errors.New("remote project has no origin")
		}
		if !originMatches(strings.TrimSpace(string(origin)), manifest.Project.Repository) {
			return errors.New("origin does not match manifest repository")
		}
	}
	fmt.Fprintf(stdout, "doctor ok: %d manifest hashes verified; CURRENT-STATE is current\n", len(manifest.Files))
	return nil
}

func originMatches(origin, repository string) bool {
	normalized := strings.TrimSuffix(strings.ReplaceAll(origin, "\\", "/"), ".git")
	return strings.HasSuffix(strings.ToLower(normalized), "/"+strings.ToLower(repository)) ||
		strings.HasSuffix(strings.ToLower(normalized), ":"+strings.ToLower(repository))
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func lookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("required executable %s was not found", name)
	}
	return path, nil
}

func runOutput(directory, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if directory != "" {
		cmd.Dir = directory
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}
