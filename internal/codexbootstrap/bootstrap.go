// Package codexbootstrap manages the optional Context Bridge block in the
// user's Codex global AGENTS.md file.
package codexbootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

const (
	BeginMarker = "<!-- BEGIN CONTEXT BRIDGE MANAGED BLOCK -->"
	EndMarker   = "<!-- END CONTEXT BRIDGE MANAGED BLOCK -->"
	managedBody = "## Context Bridge\n\nWhen a repository contains Context Bridge instructions, read the applicable `AGENTS.md`, `docs/agent/START-HERE.md`, and `docs/agent/CURRENT-STATE.md` before editing. Treat repository source, tests, CI, and Git state as authoritative; preserve unrelated changes and run documented verification before reporting."
)

type Status string

const (
	StatusReady      Status = "READY"
	StatusMissing    Status = "MISSING"
	StatusOverride   Status = "OVERRIDE_PRESENT"
	StatusConflict   Status = "CONFLICT"
	StatusUnreadable Status = "UNREADABLE"
)

type Report struct {
	Path               string      `json:"path"`
	Status             Status      `json:"status"`
	Exists             bool        `json:"exists"`
	ManagedBlock       bool        `json:"managed_block"`
	OverridePath       string      `json:"override_path,omitempty"`
	OverridePresent    bool        `json:"override_present"`
	Warnings           []string    `json:"warnings,omitempty"`
	SafeNextAction     string      `json:"safe_next_action"`
	ContentFingerprint string      `json:"-"`
	FileMode           os.FileMode `json:"-"`
}

type Plan struct {
	Path            string
	Content         string
	Action          string
	Directory       string
	CreateDirectory bool
	Existing        bool
	ExistingHash    string
	ExistingMode    os.FileMode
	Fingerprint     string
	Warnings        []string
	SafeNextAction  string
}

func ManagedBlock() string {
	return BeginMarker + "\n" + managedBody + "\n" + EndMarker + "\n"
}

func GlobalPath() (string, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home for Codex: %w", err)
		}
		codexHome = filepath.Join(home, ".codex")
	}
	if !filepath.IsAbs(codexHome) {
		return "", errors.New("CODEX_HOME must be an absolute path")
	}
	return filepath.Join(filepath.Clean(codexHome), "AGENTS.md"), nil
}

func OverridePath(path string) string { return filepath.Join(filepath.Dir(path), "AGENTS.override.md") }

func Inspect() (Report, error) {
	path, err := GlobalPath()
	if err != nil {
		return Report{}, err
	}
	return inspectPath(path)
}

func inspectPath(path string) (Report, error) {
	report := Report{Path: path, Status: StatusMissing, Warnings: []string{}, SafeNextAction: "run contextbridge setup --codex-bootstrap --confirm"}
	if err := validateParent(path); err != nil {
		return Report{}, err
	}
	overridePath := OverridePath(path)
	if info, err := os.Lstat(overridePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return Report{}, errors.New("Codex AGENTS.override.md is a symlink or reparse point")
		}
		data, readErr := os.ReadFile(overridePath)
		if readErr != nil {
			return Report{}, fmt.Errorf("read Codex AGENTS.override.md: %w", readErr)
		}
		if strings.TrimSpace(string(data)) != "" {
			report.OverridePath = overridePath
			report.OverridePresent = true
			report.Status = StatusOverride
			report.Warnings = append(report.Warnings, "CODEX_GLOBAL_OVERRIDE_PRESENT")
			report.SafeNextAction = "review the Codex global AGENTS.override.md; Codex uses it instead of AGENTS.md"
			return report, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Report{}, fmt.Errorf("inspect Codex global AGENTS.override.md: %w", err)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return Report{}, fmt.Errorf("inspect Codex global AGENTS.md: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Report{}, errors.New("Codex global AGENTS.md is a symlink or reparse point")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, fmt.Errorf("read Codex global AGENTS.md: %w", err)
	}
	report.Exists = true
	report.ContentFingerprint = hash(data)
	report.FileMode = info.Mode().Perm()
	start, end, markerErr := markerRange(string(data))
	if markerErr != nil {
		report.Status = StatusConflict
		report.Warnings = append(report.Warnings, "CODEX_GLOBAL_MANAGED_BLOCK_CONFLICT")
		report.SafeNextAction = "review the malformed Context Bridge block in the Codex global AGENTS.md manually"
		return report, nil
	}
	if start < 0 {
		return report, nil
	}
	report.ManagedBlock = true
	if normalizeNewlines(string(data[start:end])) == strings.TrimSuffix(ManagedBlock(), "\n") {
		report.Status = StatusReady
		report.SafeNextAction = "none; the Context Bridge Codex global router is ready"
	} else {
		report.Status = StatusConflict
		report.Warnings = append(report.Warnings, "CODEX_GLOBAL_MANAGED_BLOCK_CHANGED")
		report.SafeNextAction = "review the existing Context Bridge block before changing it"
	}
	return report, nil
}

func PlanBootstrap() (Plan, error) {
	path, err := GlobalPath()
	if err != nil {
		return Plan{}, err
	}
	return planPath(path)
}

func planPath(path string) (Plan, error) {
	if err := validateParent(path); err != nil {
		return Plan{}, err
	}
	report, err := inspectPath(path)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Path: path, Action: "create", Directory: filepath.Dir(path), Warnings: append([]string{}, report.Warnings...), SafeNextAction: report.SafeNextAction}
	if report.Status == StatusOverride || report.Status == StatusConflict {
		plan.Action = "blocked"
		return plan, nil
	}
	if report.Exists {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return Plan{}, readErr
		}
		plan.Existing = true
		plan.ExistingHash = hash(data)
		plan.ExistingMode = report.FileMode
		start, end, markerErr := markerRange(string(data))
		if markerErr != nil {
			plan.Action = "blocked"
			return plan, nil
		}
		if start < 0 {
			plan.Content = appendBlock(string(data))
			plan.Action = "update"
		} else {
			plan.Content = replaceBlock(string(data), start, end)
			if plan.Content == string(data) {
				plan.Action = "none"
			}
		}
	} else {
		plan.Content = ManagedBlock()
		if _, statErr := os.Lstat(plan.Directory); errors.Is(statErr, os.ErrNotExist) {
			plan.CreateDirectory = true
		} else if statErr != nil {
			return Plan{}, fmt.Errorf("inspect Codex home: %w", statErr)
		}
	}
	plan.Fingerprint = planFingerprint(plan)
	if plan.Action == "none" {
		plan.SafeNextAction = "none; the Context Bridge Codex global router is ready"
	}
	return plan, nil
}

func Apply(plan Plan) error {
	if plan.Action == "none" {
		return nil
	}
	if plan.Action == "blocked" {
		return errors.New("refusing to modify the Codex global instruction file while a conflict is present")
	}
	current, err := planPath(plan.Path)
	if err != nil {
		return err
	}
	if current.Fingerprint != plan.Fingerprint || current.Action != plan.Action || current.Content != plan.Content {
		return errors.New("Codex global instructions changed after preview; generate and review a new setup preview")
	}
	if plan.CreateDirectory {
		if err := os.Mkdir(plan.Directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create Codex home: %w", err)
		}
	}
	mode := plan.ExistingMode
	if mode == 0 {
		mode = 0o600
	}
	if err := registry.AtomicReplaceFile(plan.Path, []byte(plan.Content), mode); err != nil {
		return fmt.Errorf("write Codex global AGENTS.md: %w", err)
	}
	return nil
}

func markerRange(data string) (int, int, error) {
	start := strings.Index(data, BeginMarker)
	endMarker := strings.Index(data, EndMarker)
	if start < 0 && endMarker < 0 {
		return -1, -1, nil
	}
	if start < 0 || endMarker < 0 || endMarker < start || strings.Count(data, BeginMarker) != 1 || strings.Count(data, EndMarker) != 1 {
		return -1, -1, errors.New("managed block markers are incomplete or duplicated")
	}
	return start, endMarker + len(EndMarker), nil
}

func appendBlock(data string) string {
	if data == "" {
		return ManagedBlock()
	}
	lineEnding := "\n"
	if strings.Contains(data, "\r\n") {
		lineEnding = "\r\n"
	}
	if !strings.HasSuffix(data, "\n") {
		data += lineEnding
	}
	block := strings.ReplaceAll(ManagedBlock(), "\n", lineEnding)
	return data + lineEnding + block
}

func replaceBlock(data string, start, end int) string {
	lineEnding := "\n"
	if strings.Contains(data, "\r\n") {
		lineEnding = "\r\n"
	}
	block := strings.ReplaceAll(ManagedBlock(), "\n", lineEnding)
	return data[:start] + block[:len(block)-len(lineEnding)] + data[end:]
}

func normalizeNewlines(value string) string { return strings.ReplaceAll(value, "\r\n", "\n") }

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func planFingerprint(plan Plan) string {
	value := fmt.Sprintf("%s\x00%s\x00%s\x00%s", plan.Path, plan.Action, plan.ExistingHash, plan.Content)
	return hash([]byte(value))
}

func validateParent(path string) error {
	current := filepath.Clean(filepath.Dir(path))
	for {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
			continue
		}
		if err != nil {
			return errors.New("Codex global instruction parent could not be inspected")
		}
		if !info.IsDir() {
			return errors.New("Codex global instruction parent is not a directory")
		}
		if linked, linkErr := safety.IsLinkOrReparse(current); linkErr != nil || linked {
			return errors.New("Codex global instruction parent is a symlink or reparse point")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}
