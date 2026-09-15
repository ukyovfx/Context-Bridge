package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

func git(t *testing.T, directory string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Context Bridge Tests", "GIT_AUTHOR_EMAIL=contextbridge@example.invalid",
		"GIT_COMMITTER_NAME=Context Bridge Tests", "GIT_COMMITTER_EMAIL=contextbridge@example.invalid",
	)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, data)
	}
	return strings.TrimSpace(string(data))
}

func createRepository(t *testing.T, path, remote string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, path, "init", "-b", "main")
	git(t, path, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, path, "add", "tracked.txt")
	git(t, path, "commit", "-m", "base")
}

func TestProbeClassifiesStatusTopologyAndPrimaryRemote(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	createRepository(t, repo, "git@GitHub.com:ukyovfx/Context-Bridge.git")
	git(t, repo, "remote", "add", "secondary", "https://example.com/other/repository.git")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("unstaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := (Prober{}).Probe(repo)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Topology != core.TopologyMainWorktree || !probe.Staged || !probe.Unstaged || !probe.Untracked {
		t.Fatalf("unexpected probe: topology=%s staged=%v unstaged=%v untracked=%v", probe.Topology, probe.Staged, probe.Unstaged, probe.Untracked)
	}
	if probe.PrimaryRemote == nil || probe.PrimaryRemote.Key() != "github.com/ukyovfx/Context-Bridge" {
		t.Fatalf("unexpected primary remote: %#v", probe.PrimaryRemote)
	}
	if len(probe.Remotes) != 2 {
		t.Fatalf("secondary remote was not inventoried: %#v", probe.Remotes)
	}

	worktree := filepath.Join(root, "linked")
	git(t, repo, "worktree", "add", "-b", "linked-test", worktree)
	linked, err := (Prober{}).Probe(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Topology != core.TopologyLinkedWorktree || linked.GitCommonDirKey != probe.GitCommonDirKey {
		t.Fatalf("linked worktree not recognized: %#v", linked)
	}
}

func TestProbeNormalRepositoryHasCompleteEvidence(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	createRepository(t, repo, "https://github.com/example/repo.git")
	probe, err := (Prober{}).Probe(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.EvidenceErrors) != 0 {
		t.Fatalf("normal repository produced incomplete evidence: %v; probe=%#v", probe.EvidenceErrors, probe)
	}
}

func TestProbeDetachedAndLocalUniqueWork(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	createRepository(t, repo, "https://github.com/ukyovfx/Context-Bridge.git")
	git(t, repo, "checkout", "--detach")
	if err := os.WriteFile(filepath.Join(repo, "detached.txt"), []byte("detached work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "detached.txt")
	git(t, repo, "commit", "-m", "detached unique commit")
	probe, err := (Prober{}).Probe(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !probe.Detached || probe.Branch != "" {
		t.Fatalf("detached HEAD not detected: %#v", probe)
	}
	if probe.LocalUniqueCount < 2 {
		t.Fatalf("detached local unique commit was not detected: %d", probe.LocalUniqueCount)
	}
}

func TestProbeWithStatusDistinguishesNonGitAndInaccessibleGit(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	_, status, err := (Prober{}).ProbeWithStatus(plain)
	if err != nil || status != ProbeTargetNotGit {
		t.Fatalf("plain directory classification = %s, %v", status, err)
	}
	untrusted := filepath.Join(t.TempDir(), "untrusted")
	if err := os.MkdirAll(filepath.Join(untrusted, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, status, err = (Prober{}).ProbeWithStatus(untrusted)
	if err != nil || status != ProbeGitRepositoryInaccessible {
		t.Fatalf("Git directory classification = %s, %v", status, err)
	}
}

func TestCommandRunnerNeutralizesGitRepositorySelectors(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	other := filepath.Join(root, "other")
	createRepository(t, target, "https://github.com/example/target.git")
	createRepository(t, other, "https://github.com/example/other.git")
	selectors := map[string]string{
		"GIT_DIR":        filepath.Join(other, ".git"),
		"GIT_WORK_TREE":  other,
		"GIT_INDEX_FILE": filepath.Join(other, ".git", "index"),
	}
	for name, value := range selectors {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			probe, err := (Prober{}).Probe(target)
			if err != nil {
				t.Fatal(err)
			}
			if !equivalentProbePath(t, probe.GitRoot, target) || probe.PrimaryRemote == nil || probe.PrimaryRemote.Path != "example/target" {
				t.Fatalf("%s redirected probe: %#v", name, probe)
			}
		})
	}
}

func TestEquivalentWindowsPathsShareFilesystemIdentity(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, ".", "nested")
	canonicalNested, err := CanonicalPath(nested)
	if err != nil {
		t.Fatal(err)
	}
	canonicalAlias, err := CanonicalPath(alias)
	if err != nil {
		t.Fatal(err)
	}
	if !equivalentProbePath(t, canonicalNested, canonicalAlias) {
		t.Fatalf("equivalent lexical paths produced different keys: %q != %q", canonicalNested, canonicalAlias)
	}
	if runtime.GOOS != "windows" {
		return
	}
	shortRoot := strings.TrimSpace(gitShortPath(t, root))
	if !strings.Contains(shortRoot, "~") {
		t.Skipf("Windows short-path alias unavailable for %q", root)
	}
	shortNested := filepath.Join(shortRoot, "nested")
	canonicalShort, err := CanonicalPath(shortNested)
	if err != nil {
		t.Fatal(err)
	}
	if !equivalentProbePath(t, canonicalNested, canonicalShort) {
		t.Fatalf("equivalent long/short paths produced different keys: %q != %q", canonicalNested, canonicalShort)
	}
}

func equivalentProbePath(t *testing.T, left, right string) bool {
	t.Helper()
	leftCanonical, err := CanonicalPath(left)
	if err != nil {
		t.Fatal(err)
	}
	rightCanonical, err := CanonicalPath(right)
	if err != nil {
		t.Fatal(err)
	}
	if PathKey(leftCanonical) == PathKey(rightCanonical) {
		return true
	}
	if !safety.FilesystemIdentitySupported() {
		return false
	}
	leftIdentity, err := safety.FilesystemIdentity(leftCanonical)
	if err != nil {
		t.Fatal(err)
	}
	rightIdentity, err := safety.FilesystemIdentity(rightCanonical)
	if err != nil {
		t.Fatal(err)
	}
	return leftIdentity == rightIdentity
}

func gitShortPath(t *testing.T, path string) string {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "for %I in (\""+path+"\") do @echo %~sI")
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("resolve short path %q: %v", path, err)
	}
	return string(data)
}

func TestCommandRunnerTimeoutIsExplicit(t *testing.T) {
	runner := CommandRunner{Timeout: time.Nanosecond}
	_, err := runner.Output(t.TempDir(), "rev-parse", "--show-toplevel")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v, want context deadline exceeded", err)
	}
}

func TestProbeDetectsStashAndUniqueLocalTags(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	createRepository(t, repo, "https://github.com/ukyovfx/Context-Bridge.git")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("stashed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "stash", "push", "-m", "saved local work")
	if err := os.WriteFile(filepath.Join(repo, "tagged.txt"), []byte("tagged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "tagged.txt")
	git(t, repo, "commit", "-m", "local tagged work")
	git(t, repo, "tag", "local-only")
	probe, err := (Prober{}).Probe(repo)
	if err != nil {
		t.Fatal(err)
	}
	if probe.StashCount == 0 || probe.LocalTagUniqueCount == 0 {
		t.Fatalf("local unique work was incomplete: stash=%d tag=%d probe=%#v", probe.StashCount, probe.LocalTagUniqueCount, probe)
	}
	if !containsWorkspaceState(probe.States(), core.StateHasLocalUniqueWork) {
		t.Fatalf("unique-work state missing: %#v", probe.States())
	}
}

func TestValidateIdentityPathRejectsWindowsReparseAlias(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows reparse-point coverage")
	}
	realRoot := t.TempDir()
	linkParent := t.TempDir()
	alias := filepath.Join(linkParent, "alias")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", alias, realRoot)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v: %s", err, data)
	}
	if err := ValidateIdentityPath(alias); err == nil {
		t.Fatal("reparse alias was accepted")
	}
}

func containsWorkspaceState(states []core.WorkspaceState, wanted core.WorkspaceState) bool {
	for _, state := range states {
		if state == wanted {
			return true
		}
	}
	return false
}

func TestDiscoveryFindsIndependentClonesWithoutMutation(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one")
	two := filepath.Join(root, "two")
	createRepository(t, one, "https://github.com/ukyovfx/Context-Bridge.git")
	createRepository(t, two, "ssh://git@github.com/ukyovfx/Context-Bridge.git")
	if err := os.WriteFile(filepath.Join(one, "only-one.txt"), []byte("local evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, one, "add", "only-one.txt")
	git(t, one, "commit", "-m", "only in first clone")
	beforeOne := git(t, one, "status", "--porcelain=v2", "--branch")
	beforeTwo := git(t, two, "status", "--porcelain=v2", "--branch")
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root, Registry: core.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(result.Items))
	}
	for _, item := range result.Items {
		if item.Probe.Topology != core.TopologyIndependentClone || item.Role != core.RoleUnregistered {
			t.Fatalf("unexpected discovery item: topology=%s role=%s", item.Probe.Topology, item.Role)
		}
		if item.RelatedCloneCount != 1 {
			t.Fatalf("related clone evidence missing: %#v", item)
		}
	}
	if result.Items[0].RefOIDsOnlyHereCount == 0 && result.Items[1].RefOIDsOnlyHereCount == 0 {
		t.Fatal("cross-clone local ref evidence was not compared")
	}
	if after := git(t, one, "status", "--porcelain=v2", "--branch"); after != beforeOne {
		t.Fatal("discovery changed first repository")
	}
	if after := git(t, two, "status", "--porcelain=v2", "--branch"); after != beforeTwo {
		t.Fatal("discovery changed second repository")
	}
}

func TestDiscoveryRejectsLinkedRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction coverage")
	}
	realRoot := t.TempDir()
	linkParent := t.TempDir()
	junction := filepath.Join(linkParent, "junction")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", junction, realRoot)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v: %s", err, data)
	}
	linked, linkErr := safety.IsLinkOrReparse(junction)
	if linkErr != nil {
		t.Fatal(linkErr)
	}
	if !linked {
		t.Fatalf("junction was not identified as reparse point")
	}
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: junction})
	if err != nil || result.Status != DiscoveryFailed || len(result.Errors) != 1 || result.Errors[0].Reason != ReasonReparsePointSkipped {
		t.Fatalf("discovery did not report reparse root: result=%#v err=%v", result, err)
	}
}

func TestDiscoveryReportsNestedReparsePointSkipped(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction coverage")
	}
	root := t.TempDir()
	realTarget := t.TempDir()
	junction := filepath.Join(root, "junction")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", junction, realTarget)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v: %s", err, data)
	}
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DiscoveryPartial || !hasDiscoveryReason(result.Warnings, ReasonReparsePointSkipped) || result.SkippedCount < 1 {
		t.Fatalf("nested reparse point was not reported: %#v", result)
	}
}
