package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

type InitRequest struct {
	Name               string
	Root               string
	Owner              string
	Profile            string
	LocalOnly          bool
	GeneratorVersion   string
	ProjectID          string
	RepositoryID       string
	RepositoryIdentity *RemoteIdentity
	PrimaryRemoteName  string
	CanonicalBranch    string
}

type InitSnapshot struct {
	RootExists           bool
	RootIsDirectory      bool
	RootIsFilesystemRoot bool
	RootHasReparsePoint  bool
	TargetExists         bool
	InsideGitRepository  bool
}

type Manifest struct {
	SchemaVersion int                 `json:"schema_version"`
	Generator     Generator           `json:"generator"`
	Project       ManifestProject     `json:"project"`
	Repository    *ManifestRepository `json:"repository_identity,omitempty"`
	Lifecycle     Lifecycle           `json:"lifecycle"`
	Files         []ManifestFile      `json:"files"`
}

type Generator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ManifestProject struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Repository string `json:"repository,omitempty"`
	Profile    string `json:"profile"`
}

type ManifestRepository struct {
	ID                string         `json:"id"`
	Identity          RemoteIdentity `json:"identity"`
	PrimaryRemoteName string         `json:"primary_remote_name"`
	CanonicalBranch   string         `json:"canonical_branch"`
}

type Lifecycle struct {
	CreatedByContextBridge bool `json:"created_by_contextbridge"`
	LocalOnly              bool `json:"local_only"`
}

type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind,omitempty"`
}

func BuildInitPlan(req InitRequest, snapshot InitSnapshot) (Plan, error) {
	if err := validateInit(req, snapshot); err != nil {
		return Plan{}, err
	}
	target := filepath.Join(req.Root, req.Name)
	files := generatedFiles(req)
	manifest, err := buildManifest(req, files)
	if err != nil {
		return Plan{}, err
	}
	files[".contextbridge/manifest.json"] = manifest

	dirs := []string{".", ".contextbridge", "docs", "docs/agent", "docs/agent/plans", "docs/agent/plans/active"}
	ops := make([]Operation, 0, len(dirs)+len(files)+4)
	for _, dir := range dirs {
		ops = append(ops, Operation{Kind: CreateDirectory, Path: dir})
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		ops = append(ops, Operation{Kind: WriteFile, Path: path, Content: files[path]})
	}
	ops = append(ops,
		Operation{Kind: InitializeGit},
		Operation{Kind: StageGitFiles},
		Operation{Kind: CommitGitFiles, Args: []string{"Initialize " + req.Name + " with Context Bridge"}},
	)
	if !req.LocalOnly {
		ops = append(ops, Operation{Kind: CreatePrivateAndPush, Args: []string{req.Owner + "/" + req.Name}})
	}
	return NewPlan(target, ops), nil
}

func validateInit(req InitRequest, snapshot InitSnapshot) error {
	if !projectNamePattern.MatchString(req.Name) || strings.HasSuffix(req.Name, ".") || strings.HasSuffix(req.Name, " ") {
		return errors.New("safety abort: project name is not a safe portable repository name")
	}
	base := strings.ToUpper(strings.Split(req.Name, ".")[0])
	if windowsReservedNames[base] {
		return errors.New("safety abort: project name is reserved on Windows")
	}
	if req.Profile != "core" && req.Profile != "openai" {
		return errors.New("profile must be core or openai")
	}
	if err := ValidateID(req.ProjectID, ProjectIDPrefix); err != nil {
		return fmt.Errorf("safety abort: %w", err)
	}
	if err := ValidateID(req.RepositoryID, RepositoryIDPrefix); err != nil {
		return fmt.Errorf("safety abort: %w", err)
	}
	if req.CanonicalBranch == "" {
		return errors.New("safety abort: canonical branch is required")
	}
	if !req.LocalOnly {
		if req.RepositoryIdentity == nil || req.RepositoryIdentity.Validate() != nil || req.PrimaryRemoteName == "" {
			return errors.New("safety abort: normalized primary repository identity is required")
		}
	}
	if !req.LocalOnly && req.Owner == "" {
		return errors.New("safety abort: a verified personal GitHub owner is required")
	}
	if !snapshot.RootExists || !snapshot.RootIsDirectory {
		return errors.New("safety abort: projects root must already exist and be a directory")
	}
	if snapshot.RootIsFilesystemRoot {
		return errors.New("safety abort: filesystem root cannot be used as the projects root")
	}
	if snapshot.RootHasReparsePoint {
		return errors.New("safety abort: projects root contains a symlink or reparse point")
	}
	if snapshot.TargetExists {
		return errors.New("safety abort: target already exists; existing repositories cannot be adopted or migrated")
	}
	if snapshot.InsideGitRepository {
		return errors.New("safety abort: projects root is inside another Git repository")
	}
	return nil
}

func generatedFiles(req InitRequest) map[string]string {
	files := map[string]string{
		".gitattributes":                   "* text=auto eol=lf\n*.bat text eol=crlf\n*.cmd text eol=crlf\n*.ps1 text eol=crlf\n",
		".gitignore":                       ".env\n.env.*\n!.env.example\n.contextbridge/tmp/\n.DS_Store\nThumbs.db\n",
		"AGENTS.md":                        generatedAgents(req.Name),
		"docs/agent/START-HERE.md":         generatedStartHere(req.Name),
		"docs/agent/CURRENT-STATE.md":      generatedCurrentState(req.Name),
		"docs/agent/plans/active/.gitkeep": "",
	}
	if req.Profile == "openai" {
		files["CHATGPT-PROJECT-INSTRUCTIONS.md"] = generatedChatGPTInstructions(req.Name)
	}
	return files
}

func buildManifest(req InitRequest, files map[string]string) (string, error) {
	repository := req.Name
	if req.Owner != "" {
		repository = req.Owner + "/" + req.Name
	}
	m := Manifest{
		SchemaVersion: 2,
		Generator:     Generator{Name: "contextbridge", Version: req.GeneratorVersion},
		Project:       ManifestProject{ID: req.ProjectID, Name: req.Name, Repository: repository, Profile: req.Profile},
		Lifecycle:     Lifecycle{CreatedByContextBridge: true, LocalOnly: req.LocalOnly},
	}
	identity := RemoteIdentity{}
	if req.RepositoryIdentity != nil {
		identity = *req.RepositoryIdentity
	}
	m.Repository = &ManifestRepository{ID: req.RepositoryID, Identity: identity, PrimaryRemoteName: req.PrimaryRemoteName, CanonicalBranch: req.CanonicalBranch}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if path == "docs/agent/CURRENT-STATE.md" {
			continue
		}
		sum := sha256.Sum256([]byte(files[path]))
		m.Files = append(m.Files, ManifestFile{Path: path, SHA256: hex.EncodeToString(sum[:]), Kind: "generated_template"})
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	return string(append(b, '\n')), nil
}

func generatedAgents(name string) string {
	return fmt.Sprintf("# %s Agent Instructions\n\nRead `docs/agent/START-HERE.md` first. Keep changes scoped to this repository, preserve unrelated work, and treat repository code, tests, CI, and runtime evidence as technical truth.\n\nBefore writing in a registered workspace, run `contextbridge guard --project %s` and stop on `WRONG_WORKSPACE`.\n\nNever store secrets or tokens in repository files. Stop before destructive Git operations, production writes, permission changes, history rewriting, or unresolved high-impact decisions.\n", name, name)
}

func generatedStartHere(name string) string {
	return fmt.Sprintf("# %s Agent Start Here\n\n1. Read `AGENTS.md`.\n2. Read `docs/agent/CURRENT-STATE.md`.\n3. Read only the relevant active plan under `docs/agent/plans/active/`.\n4. Verify claims against code, tests, CI, and runtime evidence.\n\nThis project was created by Context Bridge. Obsidian, if used, is a passive Markdown interface only.\n", name)
}

func generatedCurrentState(name string) string {
	return fmt.Sprintf("---\ncontextbridge_state_schema: 1\nbasis_branch: main\nbasis_commit: \"\"\nbasis_date: \"\"\n---\n\n# %s Current State\n\nStatus: initialized; basis not yet verified\n\n## Verified state\n\n- New local Git repository created by Context Bridge.\n- No implementation milestone has been recorded yet.\n\n## Next action\n\nRecord accepted state after reviewing a product commit.\n", name)
}

func generatedChatGPTInstructions(name string) string {
	return fmt.Sprintf("# ChatGPT Project Instructions\n\nThis ChatGPT Project is exclusively for `%s`. Use the connected repository, `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and relevant code, tests, CI, and repository documentation as authoritative sources. Treat chat context as unverified until repository evidence confirms it.\n\nChatGPT handles research, requirements, planning, review, and Codex prompt generation. Codex handles repository inspection, implementation, verification, Git operations, and durable write-back.\n", name)
}
