package workspace

import (
	"bytes"
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

	"github.com/ukyovfx/Context-Bridge/internal/core"
)

type GitRunner interface {
	Output(directory string, args ...string) ([]byte, error)
}

type CommandRunner struct{}

func (CommandRunner) Output(directory string, args ...string) ([]byte, error) {
	base := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	return cmd.Output()
}

type Prober struct {
	Runner            GitRunner
	PrimaryRemoteName string
}

func (p Prober) Probe(path string) (core.WorkspaceProbe, error) {
	if p.Runner == nil {
		p.Runner = CommandRunner{}
	}
	if p.PrimaryRemoteName == "" {
		p.PrimaryRemoteName = "origin"
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
		return core.WorkspaceProbe{}, errors.New("git_dir_unavailable")
	}
	probe.GitDir, err = canonicalOutputPath(gitDir)
	if err != nil {
		return core.WorkspaceProbe{}, err
	}
	probe.GitDirKey = PathKey(probe.GitDir)
	commonDir, err := p.output(canonical, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return core.WorkspaceProbe{}, errors.New("git_common_dir_unavailable")
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
	probe.Fingerprint = fingerprint(probe)
	return probe, nil
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
