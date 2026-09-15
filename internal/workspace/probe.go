package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

type GitRunner interface {
	Output(directory string, args ...string) ([]byte, error)
}

type ProbeStatus string

const (
	ProbeOK                        ProbeStatus = "OK"
	ProbeTargetNotFound            ProbeStatus = "TARGET_NOT_FOUND"
	ProbeTargetNotGit              ProbeStatus = "TARGET_NOT_GIT"
	ProbeGitRepositoryInaccessible ProbeStatus = "GIT_REPOSITORY_INACCESSIBLE"
	ProbeGitProbeFailed            ProbeStatus = "GIT_PROBE_FAILED"
)

const defaultProbeTimeout = 10 * time.Second

type CommandRunner struct {
	Timeout time.Duration
}

func (r CommandRunner) Output(directory string, args ...string) ([]byte, error) {
	base := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = directory
	cmd.Env = sanitizedGitEnvironment()
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return output, err
}

func sanitizedGitEnvironment() []string {
	blocked := map[string]bool{"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true}
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && blocked[strings.ToUpper(name)] {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
}

type Prober struct {
	Runner            GitRunner
	PrimaryRemoteName string
}

// ProbeWithStatus adds a stable, user-facing classification without changing
// the existing probe shape or bypassing Git's own trust checks.
func (p Prober) ProbeWithStatus(path string) (core.WorkspaceProbe, ProbeStatus, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return core.WorkspaceProbe{}, ProbeTargetNotFound, nil
		}
		return core.WorkspaceProbe{}, ProbeGitProbeFailed, err
	}
	if err := ValidateIdentityPath(path); err != nil {
		return core.WorkspaceProbe{}, ProbeGitProbeFailed, err
	}
	canonical, err := CanonicalPath(path)
	if err != nil {
		return core.WorkspaceProbe{}, ProbeGitProbeFailed, err
	}
	probe, err := p.Probe(path)
	if err != nil {
		return core.WorkspaceProbe{}, ProbeGitProbeFailed, err
	}
	if probe.Topology == core.TopologyNonGit || probe.GitRoot == "" {
		if _, gitErr := os.Stat(filepath.Join(canonical, ".git")); gitErr == nil {
			return probe, ProbeGitRepositoryInaccessible, nil
		}
		return probe, ProbeTargetNotGit, nil
	}
	return probe, ProbeOK, nil
}

func (p Prober) Probe(path string) (core.WorkspaceProbe, error) {
	if p.Runner == nil {
		p.Runner = CommandRunner{}
	}
	if p.PrimaryRemoteName == "" {
		p.PrimaryRemoteName = "origin"
	}
	if err := ValidateIdentityPath(path); err != nil {
		return core.WorkspaceProbe{}, err
	}
	canonical, err := CanonicalPath(path)
	if err != nil {
		return core.WorkspaceProbe{}, err
	}
	probe := core.WorkspaceProbe{
		CanonicalPath:     canonical,
		PathKey:           PathKey(canonical),
		PrimaryRemoteName: p.PrimaryRemoteName,
		Topology:          core.TopologyUnknown,
	}
	gitRoot, err := p.output(canonical, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			probe.EvidenceErrors = append(probe.EvidenceErrors, "git_probe_timeout")
			probe.Fingerprint = fingerprint(probe)
			return probe, nil
		}
		probe.Topology = core.TopologyNonGit
		probe.Fingerprint = fingerprint(probe)
		return probe, nil
	}
	probe.GitRoot, err = canonicalOutputPath(gitRoot)
	if err != nil {
		return core.WorkspaceProbe{}, err
	}
	probe.GitRootKey = PathKey(probe.GitRoot)
	gitDir, err := p.output(canonical, "rev-parse", "--absolute-git-dir")
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, probeErrorCode("git_dir_unavailable", err))
		probe.Fingerprint = fingerprint(probe)
		return probe, nil
	}
	probe.GitDir, err = canonicalOutputPath(gitDir)
	if err != nil {
		return core.WorkspaceProbe{}, err
	}
	probe.GitDirKey = PathKey(probe.GitDir)
	commonDir, err := p.output(canonical, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, probeErrorCode("git_common_dir_unavailable", err))
		probe.Fingerprint = fingerprint(probe)
		return probe, nil
	}
	probe.GitCommonDir, err = canonicalOutputPath(commonDir)
	if err != nil {
		return core.WorkspaceProbe{}, err
	}
	probe.GitCommonDirKey = PathKey(probe.GitCommonDir)
	if probe.GitDirKey == probe.GitCommonDirKey {
		probe.Topology = core.TopologyMainWorktree
	} else {
		probe.Topology = core.TopologyLinkedWorktree
	}

	if head, headErr := p.output(canonical, "rev-parse", "--verify", "HEAD"); headErr == nil {
		probe.Head = strings.TrimSpace(string(head))
	} else {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "head_unavailable")
	}
	if branch, branchErr := p.output(canonical, "symbolic-ref", "--quiet", "--short", "HEAD"); branchErr == nil {
		probe.Branch = strings.TrimSpace(string(branch))
	} else {
		probe.Detached = probe.Head != ""
	}

	p.collectRemotes(&probe, canonical)
	p.collectStatus(&probe, canonical)
	p.collectRefs(&probe, canonical)
	p.collectWorktrees(&probe, canonical)
	collectFilesystemIdentity(&probe)
	probe.Fingerprint = fingerprint(probe)
	return probe, nil
}

func collectFilesystemIdentity(probe *core.WorkspaceProbe) {
	if !safety.FilesystemIdentitySupported() {
		return
	}
	entries := []struct {
		name string
		path string
		set  func(*core.FilesystemIdentity)
	}{
		{"workspace_root", probe.CanonicalPath, func(v *core.FilesystemIdentity) { probe.PhysicalIdentity.WorkspaceRoot = v }},
		{"git_root", probe.GitRoot, func(v *core.FilesystemIdentity) { probe.PhysicalIdentity.GitRoot = v }},
		{"git_dir", probe.GitDir, func(v *core.FilesystemIdentity) { probe.PhysicalIdentity.GitDir = v }},
		{"git_common_dir", probe.GitCommonDir, func(v *core.FilesystemIdentity) { probe.PhysicalIdentity.GitCommonDir = v }},
	}
	for _, entry := range entries {
		identity, err := safety.FilesystemIdentity(entry.path)
		if err != nil {
			probe.EvidenceErrors = append(probe.EvidenceErrors, "filesystem_identity_unavailable_"+entry.name)
			continue
		}
		value := core.FilesystemIdentity{VolumeSerialNumber: identity.VolumeSerialNumber, FileID: identity.FileID}
		entry.set(&value)
	}
}

func (p Prober) collectRemotes(probe *core.WorkspaceProbe, directory string) {
	data, err := p.output(directory, "remote")
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "remotes_unavailable")
		return
	}
	names := nonEmptyLines(data)
	sort.Strings(names)
	for _, name := range names {
		observation := core.RemoteObservation{Name: name}
		urls, outputErr := p.output(directory, "remote", "get-url", "--all", name)
		if outputErr != nil {
			observation.Errors = append(observation.Errors, "remote_url_unavailable")
		} else {
			for _, remoteURL := range nonEmptyLines(urls) {
				identity, normalizeErr := core.NormalizeRemote(remoteURL)
				if normalizeErr != nil {
					observation.Errors = append(observation.Errors, "unsupported_remote_url")
					continue
				}
				observation.Identities = append(observation.Identities, identity)
			}
		}
		if name == probe.PrimaryRemoteName {
			if len(observation.Identities) == 1 {
				identity := observation.Identities[0]
				probe.PrimaryRemote = &identity
			} else {
				probe.EvidenceErrors = append(probe.EvidenceErrors, "primary_remote_ambiguous")
			}
		}
		probe.Remotes = append(probe.Remotes, observation)
	}
	if probe.PrimaryRemote == nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "primary_remote_missing")
	}
}

func (p Prober) collectStatus(probe *core.WorkspaceProbe, directory string) {
	data, err := p.output(directory, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=normal")
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "status_unavailable")
		return
	}
	probe.PorcelainV2 = string(data)
	for _, record := range bytes.Split(data, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		if bytes.HasPrefix(record, []byte("? ")) {
			probe.Untracked = true
			continue
		}
		if (record[0] == '1' || record[0] == '2' || record[0] == 'u') && len(record) > 4 {
			fields := bytes.Fields(record)
			if len(fields) > 1 && len(fields[1]) == 2 {
				probe.Staged = probe.Staged || fields[1][0] != '.'
				probe.Unstaged = probe.Unstaged || fields[1][1] != '.'
			}
		}
	}
	uniqueArgs := []string{"rev-list", "--count"}
	if probe.Head != "" {
		uniqueArgs = append(uniqueArgs, "HEAD")
	}
	uniqueArgs = append(uniqueArgs, "--branches", "--not", "--remotes")
	unique, err := p.output(directory, uniqueArgs...)
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "unique_commits_unavailable")
		return
	}
	probe.LocalUniqueCount, err = strconv.Atoi(strings.TrimSpace(string(unique)))
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "unique_commits_invalid")
	}
	stash, stashErr := p.output(directory, "stash", "list", "--format=%H")
	if stashErr != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "stash_unavailable")
	} else {
		probe.StashCount = len(nonEmptyLines(stash))
	}
	tagUnique, tagErr := p.output(directory, "rev-list", "--count", "--tags", "--not", "--remotes")
	if tagErr != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "unique_tag_commits_unavailable")
	} else if probe.LocalTagUniqueCount, err = strconv.Atoi(strings.TrimSpace(string(tagUnique))); err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "unique_tag_commits_invalid")
	}
}

func (p Prober) collectRefs(probe *core.WorkspaceProbe, directory string) {
	data, err := p.output(directory, "for-each-ref", "--format=%(objectname)%00%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "refs_unavailable")
		return
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		parts := bytes.SplitN(line, []byte{0}, 2)
		if len(parts) == 2 && len(parts[0]) > 0 {
			probe.Refs = append(probe.Refs, core.RefObservation{OID: string(parts[0]), Name: string(parts[1])})
		}
	}
}

func (p Prober) collectWorktrees(probe *core.WorkspaceProbe, directory string) {
	data, err := p.output(directory, "worktree", "list", "--porcelain")
	if err != nil {
		probe.EvidenceErrors = append(probe.EvidenceErrors, "worktrees_unavailable")
		return
	}
	for _, line := range nonEmptyLines(data) {
		if strings.HasPrefix(line, "worktree ") {
			probe.RelatedWorktrees = append(probe.RelatedWorktrees, strings.TrimPrefix(line, "worktree "))
		}
	}
}

func (p Prober) output(directory string, args ...string) ([]byte, error) {
	return p.Runner.Output(directory, args...)
}

func probeErrorCode(code string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "git_probe_timeout"
	}
	return code
}

// ValidateIdentityPath rejects reparse aliases on Windows before canonicalizing
// the path. Other platforms retain their existing symlink behavior.
func ValidateIdentityPath(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	current, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for {
		_, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			parent := filepath.Dir(current)
			if parent == current {
				return nil
			}
			current = parent
			continue
		}
		if statErr != nil {
			return fmt.Errorf("inspect workspace identity path: %w", statErr)
		}
		linked, linkErr := safety.IsLinkOrReparse(current)
		if linkErr != nil {
			return fmt.Errorf("inspect workspace identity path: %w", linkErr)
		}
		if linked {
			return errors.New("workspace identity path contains a symlink or reparse point")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonical path: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func PathKey(path string) string {
	key := filepath.ToSlash(filepath.Clean(path))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

func canonicalOutputPath(data []byte) (string, error) {
	return CanonicalPath(strings.TrimSpace(string(data)))
}

func nonEmptyLines(data []byte) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			lines = append(lines, value)
		}
	}
	return lines
}

func fingerprint(probe core.WorkspaceProbe) string {
	value := struct {
		Path, Root, Dir, Common, Head, Branch, Remote string
		Detached                                      bool
	}{probe.PathKey, probe.GitRootKey, probe.GitDirKey, probe.GitCommonDirKey, probe.Head, probe.Branch, "", probe.Detached}
	if probe.PrimaryRemote != nil {
		value.Remote = probe.PrimaryRemote.Key()
	}
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
