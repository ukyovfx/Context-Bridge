package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/core"
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
	if _, err := (Discoverer{}).Discover(DiscoveryOptions{Root: junction}); err == nil {
		t.Fatal("discovery accepted junction root")
	}
}
