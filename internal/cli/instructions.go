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
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/state"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

const (
	readinessPass    = "PASS"
	readinessWarning = "WARNING"
	readinessUnknown = "UNKNOWN"
	readinessReady   = "READY"
	readinessWarned  = "READY_WITH_WARNINGS"
	readinessFailed  = "FAILED"
)

type instructionSource struct {
	Scope            string `json:"scope"`
	Path             string `json:"path"`
	Order            int    `json:"order"`
	Bytes            int    `json:"bytes"`
	SHA256           string `json:"sha256"`
	InsideRepository bool   `json:"inside_repository"`
	ChangedFromBasis string `json:"changed_from_basis"`
}

type diagnosticIssue struct {
	Reason string `json:"reason"`
}

type readinessSummary struct {
	WorkspaceGuard   string `json:"workspace_guard"`
	InstructionChain string `json:"instruction_chain"`
	OverrideFiles    string `json:"override_files"`
	InstructionSize  string `json:"instruction_size"`
	SandboxScope     string `json:"sandbox_scope"`
	GitEnvironment   string `json:"git_environment"`
	Overall          string `json:"overall"`
}

type codexDiagnostics struct {
	CodexHome                  string   `json:"codex_home,omitempty"`
	GlobalInstructionPaths     []string `json:"global_instruction_paths,omitempty"`
	GlobalInstructionsPresent  bool     `json:"global_instructions_present"`
	ProjectDocMaxBytes         int      `json:"project_doc_max_bytes,omitempty"`
	ProjectDocMaxBytesKnown    bool     `json:"project_doc_max_bytes_known"`
	CumulativeInstructionBytes int      `json:"cumulative_instruction_bytes"`
	PredictedTruncationRisk    string   `json:"predicted_truncation_risk"`
	SandboxMode                string   `json:"sandbox_mode,omitempty"`
	WritableRoots              []string `json:"writable_roots,omitempty"`
	WritableRootsKnown         bool     `json:"writable_roots_known"`
	ApprovalPolicy             string   `json:"approval_policy,omitempty"`
	SecurityConfiguration      string   `json:"security_configuration,omitempty"`
	ConfigurationObservable    bool     `json:"configuration_observable"`
}

type claudeDiagnostics struct {
	ClaudeConfigDir      string   `json:"claude_config_dir,omitempty"`
	CLAUDELocalPresent   bool     `json:"claude_local_present"`
	AGENTSImportPresent  bool     `json:"agents_import_present"`
	RulesPaths           []string `json:"rules_paths,omitempty"`
	GlobalInstructions   []string `json:"global_instructions,omitempty"`
	ManagedPolicyPresent bool     `json:"managed_policy_present"`
	AutoMemoryPresent    bool     `json:"auto_memory_present"`
}

type cursorDiagnostics struct {
	RulePaths                  []string `json:"rule_paths,omitempty"`
	WorktreeConfigurationPaths []string `json:"worktree_configuration_paths,omitempty"`
	ExternalWorktreeLocation   string   `json:"external_worktree_location,omitempty"`
	InsideExternalWorktree     bool     `json:"inside_external_worktree"`
}

type agentDiagnostics struct {
	Agent            string              `json:"agent"`
	EffectiveCWD     string              `json:"effective_cwd"`
	ProjectRoot      string              `json:"project_root"`
	InstructionChain []instructionSource `json:"instruction_chain"`
	CumulativeBytes  int                 `json:"cumulative_instruction_bytes"`
	Codex            *codexDiagnostics   `json:"codex,omitempty"`
	Claude           *claudeDiagnostics  `json:"claude,omitempty"`
	Cursor           *cursorDiagnostics  `json:"cursor,omitempty"`
	AcceptedState    state.Report        `json:"accepted_state"`
	WorkspaceGuard   string              `json:"workspace_guard"`
	Readiness        readinessSummary    `json:"readiness"`
	Warnings         []diagnosticIssue   `json:"warnings"`
	Errors           []diagnosticIssue   `json:"errors"`
}

type instructionResult struct {
	Status      string           `json:"status"`
	Diagnostics agentDiagnostics `json:"diagnostics"`
}

func runInstructions(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("instructions", flag.ContinueOnError)
	flags.SetOutput(stderr)
	agent := flags.String("agent", "codex", "codex, claude, or cursor")
	project := flags.String("project", "", "workspace or project path; defaults to the current directory")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	explain := flags.Bool("explain", false, "explain effective instruction and environment inputs")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("instructions accepts options only")
	}
	if !*explain {
		return errors.New("instructions requires --explain")
	}
	if !validAgent(*agent) {
		return errors.New("agent must be codex, claude, or cursor")
	}
	root := *project
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	result := diagnoseAgent(root, *agent)
	emitInstructionResult(stdout, result, *jsonOutput)
	if result.Status == readinessFailed {
		return errors.New("instruction diagnostics failed")
	}
	return nil
}

func validAgent(agent string) bool { return agent == "codex" || agent == "claude" || agent == "cursor" }

func diagnoseAgent(root, agent string) instructionResult {
	result := instructionResult{Status: readinessReady, Diagnostics: agentDiagnostics{Agent: agent, Warnings: []diagnosticIssue{}, Errors: []diagnosticIssue{}}}
	diagnostics := &result.Diagnostics
	canonical, err := workspace.CanonicalPath(root)
	if err != nil {
		diagnostics.Errors = append(diagnostics.Errors, diagnosticIssue{Reason: "AGENT_ROOT_MISMATCH"})
		result.Status = readinessFailed
		return result
	}
	diagnostics.EffectiveCWD = canonical
	probe, probeErr := (workspace.Prober{}).Probe(canonical)
	if probeErr != nil || probe.Topology == core.TopologyNonGit {
		diagnostics.Errors = append(diagnostics.Errors, diagnosticIssue{Reason: "PROBE_FAILED"})
		diagnostics.WorkspaceGuard = readinessUnknown
	} else {
		diagnostics.ProjectRoot = probe.GitRoot
		diagnostics.AcceptedState = state.Evaluate(probe.GitRoot, workspace.CommandRunner{})
		diagnostics.WorkspaceGuard = diagnoseGuard(probe)
	}
	if diagnostics.ProjectRoot == "" {
		diagnostics.ProjectRoot = canonical
	}
	diagnostics.InstructionChain = discoverInstructionSources(diagnostics.ProjectRoot, canonical, agent)
	for _, source := range diagnostics.InstructionChain {
		diagnostics.CumulativeBytes += source.Bytes
	}
	diagnostics.Codex, diagnostics.Claude, diagnostics.Cursor = diagnoseEnvironment(diagnostics.ProjectRoot, canonical, agent, diagnostics.InstructionChain, diagnostics.CumulativeBytes)
	deriveDiagnosticWarnings(diagnostics)
	diagnostics.Readiness = makeReadiness(diagnostics)
	if len(diagnostics.Errors) > 0 {
		result.Status = readinessFailed
	} else if diagnostics.Readiness.Overall == readinessWarned {
		result.Status = readinessWarned
	}
	return result
}

func diagnoseGuard(probe core.WorkspaceProbe) string {
	store, err := registryStore()
	if err != nil {
		return readinessUnknown
	}
	value, err := store.Load()
	if err != nil {
		return readinessUnknown
	}
	for _, registered := range value.Workspaces {
		if registered.PathKey != probe.PathKey || registered.Role != core.RoleCanonical {
			continue
		}
		project, projectErr := projectForRepository(value, registered.RepositoryID)
		var repository core.RepositoryRecord
		for _, candidate := range value.Repositories {
			if candidate.ID == registered.RepositoryID {
				repository = candidate
				break
			}
		}
		if projectErr != nil || repository.ID == "" {
			return readinessUnknown
		}
		decision := core.EvaluateGuard(guardExpected(project, repository, registered, ""), core.GuardActual{ProjectFound: true, ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, Probe: probe})
		if decision.Allowed {
			return readinessPass
		}
		return readinessWarning
	}
	for _, registered := range value.Workspaces {
		if registered.Role == core.RoleCanonical && registered.GitRootKey == probe.GitRootKey {
			return readinessWarning
		}
	}
	return readinessUnknown
}

func projectForRepository(value core.Registry, repositoryID string) (core.ProjectRecord, error) {
	for _, repository := range value.Repositories {
		if repository.ID == repositoryID {
			return resolveProject(value, repository.ProjectID)
		}
	}
	return core.ProjectRecord{}, errors.New("project is missing")
}

func discoverInstructionSources(projectRoot, cwd, agent string) []instructionSource {
	paths := make([]struct{ scope, path string }, 0)
	if agent == "codex" {
		paths = append(paths, discoverChain(projectRoot, cwd, []string{"AGENTS.md", "AGENTS.override.md"})...)
		for _, path := range codexGlobalPaths() {
			paths = append(paths, struct{ scope, path string }{"global", path})
		}
	} else if agent == "claude" {
		paths = append(paths, discoverChain(projectRoot, cwd, []string{"CLAUDE.md", "CLAUDE.local.md"})...)
		for _, path := range claudeGlobalPaths() {
			paths = append(paths, struct{ scope, path string }{"global", path})
		}
	} else {
		paths = append(paths, discoverChain(projectRoot, cwd, []string{".cursorrules"})...)
		paths = append(paths, discoverCursorRules(projectRoot)...)
	}
	sources := make([]instructionSource, 0, len(paths))
	for _, item := range paths {
		data, err := os.ReadFile(item.path)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		sources = append(sources, instructionSource{Scope: item.scope, Path: item.path, Order: len(sources) + 1, Bytes: len(data), SHA256: hex.EncodeToString(sum[:]), InsideRepository: insidePath(projectRoot, item.path), ChangedFromBasis: changedFromBasis(projectRoot, item.path)})
	}
	return sources
}

func discoverChain(projectRoot, cwd string, names []string) []struct{ scope, path string } {
	root := filepath.Clean(projectRoot)
	current := filepath.Clean(cwd)
	directories := make([]string, 0)
	for {
		directories = append(directories, current)
		if workspace.PathKey(current) == workspace.PathKey(root) || filepath.Dir(current) == current {
			break
		}
		current = filepath.Dir(current)
	}
	sort.Slice(directories, func(i, j int) bool { return pathDepth(directories[i]) < pathDepth(directories[j]) })
	paths := make([]struct{ scope, path string }, 0)
	for _, directory := range directories {
		for _, name := range names {
			paths = append(paths, struct{ scope, path string }{"project", filepath.Join(directory, name)})
		}
	}
	return paths
}

func pathDepth(path string) int {
	return len(strings.FieldsFunc(filepath.Clean(path), func(r rune) bool { return r == '/' || r == '\\' }))
}

func codexGlobalPaths() []string {
	home, _ := os.UserHomeDir()
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
		return uniquePaths([]string{filepath.Join(home, "AGENTS.md"), filepath.Join(codexHome, "AGENTS.md")})
	}
	return uniquePaths([]string{filepath.Join(codexHome, "AGENTS.md")})
}

func claudeGlobalPaths() []string {
	home, _ := os.UserHomeDir()
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = filepath.Join(home, ".claude")
		return uniquePaths([]string{filepath.Join(home, "CLAUDE.md"), filepath.Join(config, "CLAUDE.md"), filepath.Join(config, "CLAUDE.local.md")})
	}
	return uniquePaths([]string{filepath.Join(config, "CLAUDE.md"), filepath.Join(config, "CLAUDE.local.md")})
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		key := workspace.PathKey(path)
		if !seen[key] {
			seen[key] = true
			result = append(result, path)
		}
	}
	return result
}

func discoverCursorRules(root string) []struct{ scope, path string } {
	paths := make([]struct{ scope, path string }, 0)
	for _, directory := range []string{root, filepath.Join(root, ".cursor")} {
		for _, name := range []string{".cursorrules", "rules"} {
			path := filepath.Join(directory, name)
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.IsDir() {
				_ = filepath.WalkDir(path, func(entryPath string, entry os.DirEntry, walkErr error) error {
					if walkErr == nil && !entry.IsDir() {
						paths = append(paths, struct{ scope, path string }{"project_rule", entryPath})
					}
					return nil
				})
			} else {
				paths = append(paths, struct{ scope, path string }{"project_rule", path})
			}
		}
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].path < paths[j].path })
	return paths
}

func diagnoseEnvironment(root, cwd, agent string, sources []instructionSource, cumulative int) (*codexDiagnostics, *claudeDiagnostics, *cursorDiagnostics) {
	if agent == "codex" {
		codexHome := os.Getenv("CODEX_HOME")
		if codexHome == "" {
			home, _ := os.UserHomeDir()
			codexHome = filepath.Join(home, ".codex")
		}
		configuration := parseCodexConfig(filepath.Join(codexHome, "config.toml"))
		global := false
		for _, source := range sources {
			if source.Scope == "global" {
				global = true
			}
		}
		globalPaths := make([]string, 0)
		for _, source := range sources {
			if source.Scope == "global" {
				globalPaths = append(globalPaths, source.Path)
			}
		}
		return &codexDiagnostics{CodexHome: codexHome, GlobalInstructionPaths: globalPaths, GlobalInstructionsPresent: global, ProjectDocMaxBytes: configuration.maxBytes, ProjectDocMaxBytesKnown: configuration.maxBytes > 0, CumulativeInstructionBytes: cumulative, PredictedTruncationRisk: truncationRisk(cumulative, configuration.maxBytes), SandboxMode: configuration.sandbox, WritableRoots: configuration.writableRoots, WritableRootsKnown: configuration.writableRootsKnown, ApprovalPolicy: configuration.approval, SecurityConfiguration: configuration.security, ConfigurationObservable: configuration.observable}, nil, nil
	}
	if agent == "claude" {
		config := os.Getenv("CLAUDE_CONFIG_DIR")
		if config == "" {
			home, _ := os.UserHomeDir()
			config = filepath.Join(home, ".claude")
		}
		local := false
		imports := false
		global := make([]string, 0)
		for _, source := range sources {
			if strings.HasSuffix(strings.ToLower(source.Path), "claude.local.md") {
				local = true
			}
			if source.Scope == "global" {
				global = append(global, source.Path)
			}
			if data, err := os.ReadFile(source.Path); err == nil && strings.Contains(strings.ToLower(string(data)), "@agents.md") {
				imports = true
			}
		}
		rules := make([]string, 0)
		ruleRoot := filepath.Join(root, ".claude", "rules")
		_ = filepath.WalkDir(ruleRoot, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				rules = append(rules, path)
			}
			return nil
		})
		managed := false
		for _, path := range []string{filepath.Join(os.Getenv("ProgramData"), "ClaudeCode"), filepath.Join(os.Getenv("ProgramFiles"), "ClaudeCode")} {
			if path != "" {
				if _, err := os.Stat(path); err == nil {
					managed = true
				}
			}
		}
		memoryPath := filepath.Join(config, "projects", workspace.PathKey(root), "memory")
		if runtime.GOOS == "windows" {
			memoryPath = filepath.Join(config, "projects", strings.ReplaceAll(workspace.PathKey(root), "/", "-"), "memory")
		}
		_, memoryErr := os.Stat(memoryPath)
		return nil, &claudeDiagnostics{ClaudeConfigDir: config, CLAUDELocalPresent: local, AGENTSImportPresent: imports, RulesPaths: rules, GlobalInstructions: global, ManagedPolicyPresent: managed, AutoMemoryPresent: memoryErr == nil}, nil
	}
	rules := make([]string, 0)
	for _, source := range sources {
		rules = append(rules, source.Path)
	}
	worktreePaths := []string{}
	for _, path := range []string{filepath.Join(root, ".cursor", "worktrees"), filepath.Join(root, ".cursor-worktrees")} {
		if _, err := os.Stat(path); err == nil {
			worktreePaths = append(worktreePaths, path)
		}
	}
	home, _ := os.UserHomeDir()
	external := filepath.Join(home, ".cursor", "worktrees")
	inside := isWithin(cwd, external)
	return nil, nil, &cursorDiagnostics{RulePaths: rules, WorktreeConfigurationPaths: worktreePaths, ExternalWorktreeLocation: external, InsideExternalWorktree: inside}
}

type codexConfig struct {
	maxBytes                    int
	sandbox, approval, security string
	writableRoots               []string
	writableRootsKnown          bool
	observable                  bool
}

func parseCodexConfig(path string) codexConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return codexConfig{}
	}
	result := codexConfig{observable: true}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch key {
		case "project_doc_max_bytes":
			result.maxBytes, _ = strconv.Atoi(value)
		case "sandbox_mode":
			result.sandbox = value
		case "approval_policy":
			result.approval = value
		case "security":
			result.security = value
		case "writable_roots":
			result.writableRoots = parseStringArray(value)
			result.writableRootsKnown = true
		}
	}
	return result
}

func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "[]")
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.Trim(strings.TrimSpace(item), "\"")
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func truncationRisk(bytes, limit int) string {
	if limit <= 0 {
		return "UNKNOWN"
	}
	if bytes > limit {
		return "EXCEEDS_LIMIT"
	}
	if bytes*10 >= limit*9 {
		return "APPROACHING_LIMIT"
	}
	return "LOW"
}

func deriveDiagnosticWarnings(diagnostics *agentDiagnostics) {
	for _, source := range diagnostics.InstructionChain {
		if strings.Contains(strings.ToLower(filepath.Base(source.Path)), ".override.") {
			diagnostics.Warnings = append(diagnostics.Warnings, diagnosticIssue{Reason: "INSTRUCTION_OVERRIDE_PRESENT"})
		}
		if source.Scope == "global" {
			diagnostics.Warnings = append(diagnostics.Warnings, diagnosticIssue{Reason: "GLOBAL_INSTRUCTION_PRESENT"})
		}
	}
	if diagnostics.Codex != nil && diagnostics.Codex.PredictedTruncationRisk != "LOW" && diagnostics.Codex.PredictedTruncationRisk != "UNKNOWN" {
		diagnostics.Warnings = append(diagnostics.Warnings, diagnosticIssue{Reason: "INSTRUCTION_TRUNCATION_RISK"})
	}
	if diagnostics.WorkspaceGuard == readinessWarning {
		diagnostics.Warnings = append(diagnostics.Warnings, diagnosticIssue{Reason: "AGENT_ROOT_MISMATCH"})
	}
	if diagnostics.Codex != nil && diagnostics.Codex.WritableRootsKnown && len(diagnostics.Codex.WritableRoots) == 0 {
		diagnostics.Warnings = append(diagnostics.Warnings, diagnosticIssue{Reason: "AGENT_WRITABLE_ROOT_MISMATCH"})
	}
	if diagnostics.Codex != nil && (!diagnostics.Codex.ConfigurationObservable || !diagnostics.Codex.WritableRootsKnown) {
		diagnostics.Warnings = append(diagnostics.Warnings, diagnosticIssue{Reason: "AGENT_CONFIG_UNVERIFIED"})
	}
}

func makeReadiness(diagnostics *agentDiagnostics) readinessSummary {
	chain := readinessPass
	if len(diagnostics.InstructionChain) == 0 {
		chain = readinessUnknown
	}
	override := readinessPass
	for _, issue := range diagnostics.Warnings {
		if issue.Reason == "INSTRUCTION_OVERRIDE_PRESENT" {
			override = readinessWarning
		}
	}
	size := readinessPass
	if diagnostics.Codex != nil {
		if diagnostics.Codex.PredictedTruncationRisk == "UNKNOWN" {
			size = readinessUnknown
		} else if diagnostics.Codex.PredictedTruncationRisk != "LOW" {
			size = readinessWarning
		}
	}
	sandbox := readinessUnknown
	if diagnostics.Codex != nil && diagnostics.Codex.ConfigurationObservable {
		sandbox = readinessPass
		if diagnostics.Codex.WritableRootsKnown && len(diagnostics.Codex.WritableRoots) == 0 {
			sandbox = readinessWarning
		}
	}
	git := readinessPass
	if diagnostics.WorkspaceGuard == readinessUnknown {
		git = readinessUnknown
	} else if diagnostics.WorkspaceGuard == readinessWarning {
		git = readinessWarning
	}
	overall := readinessReady
	if len(diagnostics.Warnings) > 0 || sandbox == readinessWarning || size == readinessWarning {
		overall = readinessWarned
	}
	return readinessSummary{WorkspaceGuard: diagnostics.WorkspaceGuard, InstructionChain: chain, OverrideFiles: override, InstructionSize: size, SandboxScope: sandbox, GitEnvironment: git, Overall: overall}
}

func changedFromBasis(root, path string) string {
	if !insidePath(root, path) {
		return "UNKNOWN"
	}
	provenance, err := state.Parse(filepath.Join(root, "docs", "agent", "CURRENT-STATE.md"))
	if err != nil {
		return "UNKNOWN"
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.Contains(relative, "..") {
		return "UNKNOWN"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "UNKNOWN"
	}
	current := sha256.Sum256(data)
	base, err := (workspace.CommandRunner{}).Output(root, "show", provenance.BasisCommit+":"+filepath.ToSlash(relative))
	if err != nil {
		return "UNKNOWN"
	}
	old := sha256.Sum256(base)
	if current == old {
		return "UNCHANGED"
	}
	return "CHANGED"
}

func insidePath(root, path string) bool { return isWithin(path, root) }

func isWithin(path, root string) bool {
	pathKey := workspace.PathKey(path)
	rootKey := strings.TrimSuffix(workspace.PathKey(root), "/")
	return pathKey == rootKey || strings.HasPrefix(pathKey, rootKey+"/")
}

func emitInstructionResult(writer io.Writer, result instructionResult, jsonOutput bool) {
	if jsonOutput {
		data, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintln(writer, `{"status":"failed","diagnostics":{"warnings":[],"errors":[{"reason":"PROBE_FAILED"}]}}`)
			return
		}
		fmt.Fprintln(writer, string(data))
		return
	}
	d := result.Diagnostics
	fmt.Fprintf(writer, "Agent: %s\nEffective cwd: %s\nProject root: %s\n", d.Agent, d.EffectiveCWD, d.ProjectRoot)
	fmt.Fprintf(writer, "Workspace Guard: %s\nInstruction chain: %s\nOverride files: %s\nInstruction size: %s\nSandbox scope: %s\nGit environment: %s\nOverall: %s\n", d.Readiness.WorkspaceGuard, d.Readiness.InstructionChain, d.Readiness.OverrideFiles, d.Readiness.InstructionSize, d.Readiness.SandboxScope, d.Readiness.GitEnvironment, d.Readiness.Overall)
	for _, issue := range d.Warnings {
		fmt.Fprintf(writer, "warning reason=%s\n", issue.Reason)
	}
	for _, issue := range d.Errors {
		fmt.Fprintf(writer, "error reason=%s\n", issue.Reason)
	}
}
