// Package contextscan implements static, non-executing repository context checks.
package contextscan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const proposalSchema = 1

type System struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type Finding struct {
	Path string `json:"path"`
	Rule string `json:"rule"`
}
type Report struct {
	Root       string   `json:"root"`
	Mode       string   `json:"mode"`
	Systems    []System `json:"systems"`
	Canonical  []string `json:"canonical"`
	Conflicts  []string `json:"conflicts"`
	Agents     string   `json:"agents_status"`
	CodexReady bool     `json:"codex_ready"`
}
type Change struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type Proposal struct {
	Schema      int      `json:"schema"`
	Root        string   `json:"root"`
	Fingerprint string   `json:"fingerprint"`
	Changes     []Change `json:"changes"`
	Reason      string   `json:"reason"`
}
type DoctorReport struct {
	Inspection Report    `json:"inspection"`
	Findings   []Finding `json:"findings"`
}

var knownSystems = []System{
	{"AGENTS", "AGENTS.md"}, {"CLAUDE", "CLAUDE.md"}, {"CURSOR_RULES", ".cursor/rules"}, {"CURSOR_RULE", ".cursorrules"}, {"COPILOT", ".github/copilot-instructions.md"}, {"AGENT_DOCS", "docs/agent"}, {"DECISIONS", "docs/decisions"}, {"WORKSPACE", ".workspace"}, {"OPENSPEC", "openspec"}, {"SPEC_KIT", ".specify"}, {"BEADS", ".beads"}, {"TASK_MASTER", ".taskmaster"}, {"SERENA", ".serena"}, {"CI", ".github/workflows"}, {"SECURITY", "SECURITY.md"},
}

var (
	windowsUserPath = regexp.MustCompile(`(?i)\b[A-Z]:[\\/]+Users[\\/]+[^\\/\s]+`)
	privateIPv4     = regexp.MustCompile(`\b(?:10\.(?:[0-9]{1,3}\.){2}[0-9]{1,3}|192\.168\.(?:[0-9]{1,3}\.)[0-9]{1,3}|172\.(?:1[6-9]|2[0-9]|3[0-1])\.(?:[0-9]{1,3}\.)[0-9]{1,3})\b`)
	secretLike      = regexp.MustCompile(`(?im)\b(?:api[_-]?key|token|secret|password|private[_-]?key)\b\s*[:=]\s*[^\s#]+`)
	markdownLink    = regexp.MustCompile(`\]\(([^)]+)\)`)
	docPath         = regexp.MustCompile(`(?i)\b(?:AGENTS\.md|docs/agent/[A-Za-z0-9._/-]+\.md)\b`)
	basisCommit     = regexp.MustCompile(`(?m)^basis_commit:\s*['\"]?([0-9a-fA-F]{7,40})`)
)

const contextBridgeRoute = "## Context Bridge\n\nThis project uses Context Bridge. Treat this repository as the canonical source of truth. Read the repository's documented context entrypoints first, then inspect only relevant source, tests, CI, and current Git state. Prefer repository source and tests over memory or prior chat. Use Context Bridge safety checks when relevant. Preserve existing repo-native context systems and do not create duplicate state systems.\n"

func Inspect(path string) (Report, error) {
	root, err := canonicalDirectory(path)
	if err != nil {
		return Report{}, err
	}
	report := Report{Root: root, Systems: []System{}, Canonical: []string{}, Conflicts: []string{}}
	for _, system := range knownSystems {
		if exists(root, system.Path) {
			report.Systems = append(report.Systems, system)
		}
	}
	agentDocs := exists(root, "docs/agent/START-HERE.md") && exists(root, "docs/agent/CURRENT-STATE.md")
	if agentDocs {
		report.Canonical = append(report.Canonical, "docs/agent")
	}
	if exists(root, "AGENTS.md") && !agentDocs {
		report.Canonical = append(report.Canonical, "AGENTS.md")
	}
	if agentDocs && exists(root, ".workspace") {
		report.Conflicts = append(report.Conflicts, "DUPLICATE_CONTEXT_SYSTEM:docs/agent,.workspace")
	}
	if agentDocs && (exists(root, "CLAUDE.md") || exists(root, ".cursor/rules") || exists(root, ".cursorrules")) {
		report.Conflicts = append(report.Conflicts, "MULTIPLE_AGENT_ENTRYPOINTS")
	}
	report.Agents, report.CodexReady = agentsStatus(root)
	switch {
	case len(report.Conflicts) > 0:
		report.Mode = "conflict"
	case agentDocs || exists(root, "AGENTS.md") || exists(root, "CLAUDE.md") || exists(root, ".cursor/rules") || exists(root, ".cursorrules") || exists(root, ".github/copilot-instructions.md"):
		report.Mode = "preserve_existing"
	default:
		report.Mode = "greenfield"
	}
	return report, nil
}

func Preview(path string) (Proposal, error) {
	report, err := Inspect(path)
	if err != nil {
		return Proposal{}, err
	}
	proposal := Proposal{Schema: proposalSchema, Root: report.Root, Changes: []Change{}}
	if report.Agents == "conflict" || len(report.Conflicts) > 0 {
		proposal.Reason = "CONFLICTING_AGENT_INSTRUCTIONS"
		proposal.Fingerprint = fingerprint(report.Root, nil)
		return proposal, nil
	}
	if report.Mode != "greenfield" {
		proposal.Reason = "PRESERVE_EXISTING_CONTEXT"
		if report.Agents == "missing" || report.Agents == "missing_route" {
			proposal.Changes = []Change{{"AGENTS.md", routedAgents(report.Root)}}
			proposal.Fingerprint = fingerprint(report.Root, proposal.Changes)
			return proposal, nil
		}
		proposal.Fingerprint = fingerprint(report.Root, nil)
		return proposal, nil
	}
	name := filepath.Base(report.Root)
	proposal.Changes = []Change{{"AGENTS.md", agents(name)}, {"docs/agent/START-HERE.md", startHere(name)}, {"docs/agent/CURRENT-STATE.md", currentState(name)}}
	proposal.Reason = "MINIMAL_GREENFIELD_SCAFFOLD"
	proposal.Fingerprint = fingerprint(report.Root, proposal.Changes)
	return proposal, nil
}

func Apply(proposal Proposal) error {
	if proposal.Schema != proposalSchema || proposal.Root == "" {
		return errors.New("invalid Context Bridge proposal")
	}
	current, err := Preview(proposal.Root)
	if err != nil {
		return err
	}
	if current.Fingerprint != proposal.Fingerprint || !sameChanges(current.Changes, proposal.Changes) {
		return errors.New("target changed after preview; generate and review a new preview")
	}
	for _, change := range proposal.Changes {
		path := filepath.Join(proposal.Root, filepath.FromSlash(change.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if filepath.ToSlash(change.Path) == "AGENTS.md" {
			if _, err := os.Stat(path); err == nil {
				flags = os.O_WRONLY | os.O_TRUNC
			}
		}
		file, err := os.OpenFile(path, flags, 0o644)
		if err != nil {
			return fmt.Errorf("refusing to overwrite %s: %w", change.Path, err)
		}
		if _, err := file.WriteString(change.Content); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func Doctor(path string) (DoctorReport, error) {
	report, err := Inspect(path)
	if err != nil {
		return DoctorReport{}, err
	}
	result := DoctorReport{Inspection: report, Findings: []Finding{}}
	for _, conflict := range report.Conflicts {
		result.Findings = append(result.Findings, Finding{Rule: conflict})
	}
	switch report.Agents {
	case "missing":
		result.Findings = append(result.Findings, Finding{Path: "AGENTS.md", Rule: "AGENTS_MISSING"})
	case "missing_route":
		result.Findings = append(result.Findings, Finding{Path: "AGENTS.md", Rule: "AGENTS_CONTEXT_BRIDGE_MISSING"})
	case "conflict":
		result.Findings = append(result.Findings, Finding{Path: "AGENTS.md", Rule: "AGENTS_CONFLICT"})
	}
	hasVerification := false
	for _, relative := range scanPaths(report.Root) {
		data, err := os.ReadFile(filepath.Join(report.Root, filepath.FromSlash(relative)))
		if err != nil || len(data) > 256*1024 {
			continue
		}
		text := string(data)
		if relative == "AGENTS.md" && len(data) > 16*1024 {
			result.Findings = append(result.Findings, Finding{relative, "AGENTS_OVERSIZED"})
		}
		if windowsUserPath.MatchString(text) {
			result.Findings = append(result.Findings, Finding{relative, "WINDOWS_USER_PATH"})
		}
		if privateIPv4.MatchString(text) {
			result.Findings = append(result.Findings, Finding{relative, "PRIVATE_IPV4"})
		}
		if secretLike.MatchString(text) {
			result.Findings = append(result.Findings, Finding{relative, "SECRET_LIKE_CONTENT"})
		}
		lower := strings.ToLower(text)
		if strings.Contains(lower, "curl ") || strings.Contains(lower, "invoke-webrequest") {
			result.Findings = append(result.Findings, Finding{relative, "SUSPICIOUS_REMOTE_INSTRUCTION"})
		}
		if strings.Contains(lower, "test") || strings.Contains(lower, "verif") {
			hasVerification = true
		}
		for _, target := range references(text) {
			if !exists(report.Root, target) {
				result.Findings = append(result.Findings, Finding{relative, "BROKEN_REFERENCE:" + target})
			}
		}
	}
	if exists(report.Root, "AGENTS.md") && !hasVerification {
		result.Findings = append(result.Findings, Finding{"AGENTS.md", "MISSING_VERIFICATION_GUIDANCE"})
	}
	result.Findings = append(result.Findings, gitEvidenceFindings(report.Root)...)
	sort.Slice(result.Findings, func(i, j int) bool {
		return result.Findings[i].Path+result.Findings[i].Rule < result.Findings[j].Path+result.Findings[j].Rule
	})
	return result, nil
}

// gitEvidenceFindings only invokes Git's read-only plumbing. It never runs
// repository-provided hooks, configuration, scripts, or application code.
func gitEvidenceFindings(root string) []Finding {
	findings := []Finding{}
	paths := []string{"AGENTS.md", "CLAUDE.md", ".cursorrules", ".github/copilot-instructions.md", "docs/agent/START-HERE.md", "docs/agent/CURRENT-STATE.md"}
	args := append([]string{"-C", root, "status", "--porcelain=v1", "--"}, paths...)
	if output, err := exec.Command("git", args...).Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if len(line) >= 4 {
				changedPath := filepath.ToSlash(strings.TrimSpace(line[3:]))
				findings = append(findings, Finding{Path: changedPath, Rule: "UNEXPECTED_INSTRUCTION_CHANGE"})
			}
		}
	}
	statePath := filepath.Join(root, "docs", "agent", "CURRENT-STATE.md")
	state, err := os.ReadFile(statePath)
	if err != nil {
		return findings
	}
	match := basisCommit.FindStringSubmatch(string(state))
	if len(match) != 2 {
		return findings
	}
	basis := match[1]
	if err := exec.Command("git", "-C", root, "rev-parse", "--verify", basis+"^{commit}").Run(); err != nil {
		return append(findings, Finding{Path: "docs/agent/CURRENT-STATE.md", Rule: "CURRENT_STATE_BASIS_UNKNOWN"})
	}
	if err := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", basis, "HEAD").Run(); err != nil {
		return append(findings, Finding{Path: "docs/agent/CURRENT-STATE.md", Rule: "CURRENT_STATE_CONTRADICTS_GIT"})
	}
	return findings
}

func canonicalDirectory(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path must be a directory")
	}
	return abs, nil
}
func exists(root, relative string) bool {
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
	return err == nil && info.Mode()&os.ModeSymlink == 0
}
func fingerprint(root string, changes []Change) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(root))
	for _, system := range knownSystems {
		_, _ = hash.Write([]byte(system.Path))
		if exists(root, system.Path) {
			_, _ = hash.Write([]byte("1"))
		}
	}
	for _, change := range changes {
		_, _ = hash.Write([]byte(change.Path))
		_, _ = hash.Write([]byte(change.Content))
		if exists(root, change.Path) {
			_, _ = hash.Write([]byte("1"))
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}
func sameChanges(a, b []Change) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func scanPaths(root string) []string {
	paths := []string{}
	for _, relative := range []string{"AGENTS.md", "CLAUDE.md", ".cursorrules", ".github/copilot-instructions.md", "docs/agent/START-HERE.md", "docs/agent/CURRENT-STATE.md"} {
		if exists(root, relative) {
			paths = append(paths, relative)
		}
	}
	return paths
}
func references(text string) []string {
	set := map[string]bool{}
	for _, match := range markdownLink.FindAllStringSubmatch(text, -1) {
		target := strings.TrimSpace(match[1])
		if target != "" && !strings.Contains(target, "://") && !strings.HasPrefix(target, "#") && !strings.HasPrefix(target, "mailto:") {
			set[filepath.ToSlash(target)] = true
		}
	}
	for _, target := range docPath.FindAllString(text, -1) {
		set[filepath.ToSlash(target)] = true
	}
	result := make([]string, 0, len(set))
	for target := range set {
		result = append(result, target)
	}
	sort.Strings(result)
	return result
}
func agents(name string) string {
	return fmt.Sprintf("# %s Agent Instructions\n\nRead `docs/agent/START-HERE.md` first. Keep work scoped to this repository, preserve unrelated changes, and verify claims against source, tests, CI, and Git state.\n\nNever store credentials or private data in repository files. Stop before destructive Git operations, production writes, permission changes, or history rewriting.\n\n%s", name, contextBridgeRoute)
}

func routedAgents(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err == nil {
		return strings.TrimRight(string(data), "\r\n") + "\n\n" + contextBridgeRoute
	}
	return "# Agent Instructions\n\n" + contextBridgeRoute
}

func agentsStatus(root string) (string, bool) {
	path := filepath.Join(root, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "missing", false
	}
	lower := strings.ToLower(string(data))
	if strings.Contains(lower, "context bridge") || strings.Contains(lower, "contextbridge") {
		if strings.Contains(lower, "do not use context bridge") || strings.Contains(lower, "do not use contextbridge") || strings.Contains(lower, "context bridge is forbidden") {
			return "conflict", false
		}
		return "ready", true
	}
	return "missing_route", false
}
func startHere(name string) string {
	return fmt.Sprintf("# %s Agent Start Here\n\n1. Read `AGENTS.md`.\n2. Read `docs/agent/CURRENT-STATE.md`.\n3. Inspect only source, tests, CI, and Git state relevant to the task.\n\nRun the relevant tests and static checks before handoff.\n", name)
}
func currentState(name string) string {
	return fmt.Sprintf("# %s Current State\n\nStatus: initialized; no accepted implementation state recorded.\n\n## Next action\n\nReview the repository and record verified durable state when it exists.\n", name)
}
