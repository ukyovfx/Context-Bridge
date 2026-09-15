// Package localprofile manages the optional machine-local workspace layout.
// It contains placement policy only; Git and project manifests remain truth.
package localprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

const (
	Filename      = "workspace-profile-v1.json"
	SchemaVersion = 1
	maxPathLength = 4096
)

var ErrNotConfigured = errors.New("local workspace profile is not configured")

type Profile struct {
	SchemaVersion int
	WorkspaceRoot string
}

type Layout struct {
	WorkspaceRoot string
	ActiveRoot    string
	WorktreeRoot  string
	ArchiveRoot   string
	PrivateRoot   string
}

type LayoutPlan struct {
	Profile Profile
	Layout  Layout
	Create  []string
}

type Finding struct {
	Fields []string
	Code   string
}

// Load reads one optional profile without creating or modifying any file.
func Load(home string) (Profile, error) {
	data, err := os.ReadFile(filepath.Join(home, Filename))
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, ErrNotConfigured
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read local workspace profile: %w", err)
	}
	return parse(data)
}

func parse(data []byte) (Profile, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		return Profile{}, errors.New("profile must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Profile{}, errors.New("profile must contain one JSON object")
	}
	for key := range raw {
		if key != "schema_version" && key != "workspace_root" {
			return Profile{}, fmt.Errorf("unknown profile field %s", key)
		}
	}
	versionValue, ok := raw["schema_version"]
	if !ok {
		return Profile{}, errors.New("profile schema_version is required")
	}
	var version int
	if err := json.Unmarshal(versionValue, &version); err != nil || version != SchemaVersion {
		return Profile{}, errors.New("unsupported profile schema_version")
	}
	rootValue, ok := raw["workspace_root"]
	if !ok {
		return Profile{}, errors.New("profile workspace_root is required")
	}
	var root string
	if err := json.Unmarshal(rootValue, &root); err != nil || strings.TrimSpace(root) == "" {
		return Profile{}, errors.New("profile workspace_root must be a non-empty string")
	}
	root, err := normalizeAbsolute(root)
	if err != nil {
		return Profile{}, err
	}
	return Profile{SchemaVersion: version, WorkspaceRoot: root}, nil
}

func normalizeAbsolute(path string) (string, error) {
	if len(path) > maxPathLength {
		return "", errors.New("profile workspace_root is too long")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("profile workspace_root must be absolute")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("profile workspace_root could not be normalized")
	}
	return filepath.Clean(abs), nil
}

func (p Profile) Layout() Layout {
	return Layout{
		WorkspaceRoot: p.WorkspaceRoot,
		ActiveRoot:    filepath.Join(p.WorkspaceRoot, "active"),
		WorktreeRoot:  filepath.Join(p.WorkspaceRoot, "worktrees"),
		ArchiveRoot:   filepath.Join(p.WorkspaceRoot, "archive"),
		PrivateRoot:   filepath.Join(p.WorkspaceRoot, "private"),
	}
}

func SuggestedRoot() string {
	if runtime.GOOS == "windows" {
		if drive := os.Getenv("SystemDrive"); drive != "" {
			return filepath.Join(drive+string(filepath.Separator), "AI-Workspace")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "AI-Workspace")
	}
	return filepath.Join(".", "AI-Workspace")
}

// Plan validates collisions and returns only the directories that would be
// created. It performs no writes.
func Plan(root string) (LayoutPlan, error) {
	normalized, err := normalizeAbsolute(root)
	if err != nil {
		return LayoutPlan{}, err
	}
	profile := Profile{SchemaVersion: SchemaVersion, WorkspaceRoot: normalized}
	layout := profile.Layout()
	for _, path := range []string{layout.WorkspaceRoot, layout.ActiveRoot, layout.WorktreeRoot, layout.ArchiveRoot, layout.PrivateRoot} {
		if len(path) > maxPathLength {
			return LayoutPlan{}, errors.New("workspace layout path is too long")
		}
	}
	if err := validateExistingPathComponents(layout.WorkspaceRoot); err != nil {
		return LayoutPlan{}, err
	}
	if inside, checkErr := safety.InsideGitRepository(layout.WorkspaceRoot); checkErr != nil {
		return LayoutPlan{}, errors.New("workspace root Git boundary could not be checked")
	} else if inside {
		return LayoutPlan{}, errors.New("workspace root is inside an existing Git worktree")
	}
	if err := validateCreationPath(layout.WorkspaceRoot, "workspace root"); err != nil {
		return LayoutPlan{}, err
	}
	create := []string{}
	for _, entry := range []struct {
		path string
		name string
	}{{layout.WorkspaceRoot, "workspace root"}, {layout.ActiveRoot, "active root"}, {layout.WorktreeRoot, "worktree root"}, {layout.ArchiveRoot, "archive root"}, {layout.PrivateRoot, "private root"}} {
		info, statErr := os.Lstat(entry.path)
		if errors.Is(statErr, os.ErrNotExist) {
			create = append(create, entry.path)
			continue
		}
		if statErr != nil {
			return LayoutPlan{}, fmt.Errorf("inspect %s: inaccessible", entry.name)
		}
		if !info.IsDir() {
			return LayoutPlan{}, fmt.Errorf("%s is not a directory", entry.name)
		}
		if linked, linkErr := safety.IsLinkOrReparse(entry.path); linkErr != nil || linked {
			return LayoutPlan{}, fmt.Errorf("%s is a symlink or reparse point", entry.name)
		}
	}
	return LayoutPlan{Profile: profile, Layout: layout, Create: create}, nil
}

func validateCreationPath(path, label string) error {
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("parent of %s must already exist as a directory", label)
	}
	if linked, linkErr := safety.IsLinkOrReparse(parent); linkErr != nil || linked {
		return fmt.Errorf("parent of %s is a symlink or reparse point", label)
	}
	return nil
}

func validateExistingPathComponents(path string) error {
	current := filepath.Clean(path)
	for {
		_, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			parent := filepath.Dir(current)
			if parent == current {
				return nil
			}
			current = parent
			continue
		}
		if err != nil {
			return errors.New("workspace root path could not be inspected")
		}
		if linked, linkErr := safety.IsLinkOrReparse(current); linkErr != nil || linked {
			return errors.New("workspace root path contains a symlink or reparse point")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// Create creates only the paths in a previously reviewed plan. It never
// removes paths. The returned slice records paths created before any failure.
func Create(plan LayoutPlan) ([]string, error) {
	created := []string{}
	for _, path := range plan.Create {
		if err := os.Mkdir(path, 0o755); err != nil {
			return created, fmt.Errorf("create workspace directory: %w", err)
		}
		created = append(created, path)
	}
	return created, nil
}

func Save(home string, profile Profile) error {
	if profile.SchemaVersion != SchemaVersion {
		return errors.New("unsupported profile schema_version")
	}
	root, err := normalizeAbsolute(profile.WorkspaceRoot)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		SchemaVersion int    `json:"schema_version"`
		WorkspaceRoot string `json:"workspace_root"`
	}{SchemaVersion: SchemaVersion, WorkspaceRoot: root}, "", "  ")
	if err != nil {
		return errors.New("encode local workspace profile")
	}
	data = append(data, '\n')
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create Context Bridge local home: %w", err)
	}
	return registry.AtomicReplaceFile(filepath.Join(home, Filename), data, 0o600)
}

func (p Profile) LayoutFindings() []Finding {
	layout := p.Layout()
	findings := []Finding{}
	rootInfo, rootErr := os.Lstat(layout.WorkspaceRoot)
	if errors.Is(rootErr, os.ErrNotExist) {
		return append(findings, Finding{Fields: []string{"workspace_root"}, Code: "WORKSPACE_ROOT_MISSING"})
	}
	if rootErr != nil || !rootInfo.IsDir() {
		return append(findings, Finding{Fields: []string{"workspace_root"}, Code: "WORKSPACE_ROOT_NOT_DIRECTORY"})
	}
	if linked, linkErr := safety.IsLinkOrReparse(layout.WorkspaceRoot); linkErr != nil || linked {
		findings = append(findings, Finding{Fields: []string{"workspace_root"}, Code: "WORKSPACE_ROOT_REPARSE_POINT"})
	}
	for _, entry := range []struct {
		field string
		path  string
	}{
		{"active_root", layout.ActiveRoot},
		{"worktree_root", layout.WorktreeRoot},
		{"archive_root", layout.ArchiveRoot},
		{"private_root", layout.PrivateRoot},
	} {
		info, err := os.Lstat(entry.path)
		if errors.Is(err, os.ErrNotExist) {
			findings = append(findings, Finding{Fields: []string{entry.field}, Code: strings.ToUpper(entry.field) + "_MISSING"})
			continue
		}
		if err != nil || !info.IsDir() {
			findings = append(findings, Finding{Fields: []string{entry.field}, Code: strings.ToUpper(entry.field) + "_NOT_DIRECTORY"})
			continue
		}
		if linked, linkErr := safety.IsLinkOrReparse(entry.path); linkErr != nil || linked {
			findings = append(findings, Finding{Fields: []string{entry.field}, Code: strings.ToUpper(entry.field) + "_REPARSE_POINT"})
		}
	}
	return findings
}

func (p Profile) CanonicalWorkspaceFinding(path string) (Finding, bool) {
	if within(path, p.Layout().ActiveRoot) {
		return Finding{}, false
	}
	return Finding{Fields: []string{"active_root"}, Code: "CANONICAL_WORKSPACE_OUTSIDE_ACTIVE_ROOT"}, true
}

func (p Profile) ManagedWorkspaceFinding(path string) (Finding, bool) {
	if within(path, p.Layout().WorktreeRoot) {
		return Finding{}, false
	}
	return Finding{Fields: []string{"worktree_root"}, Code: "MANAGED_WORKSPACE_OUTSIDE_WORKTREE_ROOT"}, true
}

func (f Finding) FieldsText() string { return strings.Join(f.Fields, ",") }

func within(path, root string) bool {
	path = canonicalForComparison(path)
	root = canonicalForComparison(root)
	if path == root {
		return true
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func canonicalForComparison(path string) string {
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		path = canonical
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
