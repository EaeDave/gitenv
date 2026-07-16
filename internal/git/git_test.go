package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitAndStatus(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	if err := Init(root); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf("initialized .git directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Status(root)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status != "?? untracked.txt" {
		t.Fatalf("Status() = %q, want %q", status, "?? untracked.txt")
	}
}

func TestCloneCommitPushAndPullFastForward(t *testing.T) {
	temp := t.TempDir()
	remote := filepath.Join(temp, "remote.git")
	runGit(t, temp, "init", "--bare", remote)

	seed := filepath.Join(temp, "seed")
	runGit(t, temp, "clone", remote, seed)
	configureIdentity(t, seed)
	writeFile(t, filepath.Join(seed, "shared.txt"), "initial\n")
	runGit(t, seed, "add", "shared.txt")
	runGit(t, seed, "commit", "-m", "initial")
	runGit(t, seed, "push", "--set-upstream", "origin", "HEAD")

	local := filepath.Join(temp, "local")
	if err := Clone(remote, local); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	configureIdentity(t, local)
	writeFile(t, filepath.Join(local, "local.txt"), "local change\n")
	if err := CommitAndPush(local, "local commit"); err != nil {
		t.Fatalf("CommitAndPush() error = %v", err)
	}

	pushed := strings.TrimSpace(runGit(t, remote, "show", "HEAD:local.txt"))
	if pushed != "local change" {
		t.Fatalf("pushed local.txt = %q, want %q", pushed, "local change")
	}
	if err := CommitAndPush(local, "empty commit"); err == nil || !strings.Contains(err.Error(), "nothing to commit") {
		t.Fatalf("empty CommitAndPush() error = %v, want nothing to commit", err)
	}

	peer := filepath.Join(temp, "peer")
	if err := Clone(remote, peer); err != nil {
		t.Fatalf("peer Clone() error = %v", err)
	}
	configureIdentity(t, peer)
	writeFile(t, filepath.Join(peer, "shared.txt"), "fast forward\n")
	runGit(t, peer, "add", "shared.txt")
	runGit(t, peer, "commit", "-m", "remote update")
	runGit(t, peer, "push")

	before := strings.TrimSpace(runGit(t, local, "rev-parse", "HEAD"))
	if err := Pull(local); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	after := strings.TrimSpace(runGit(t, local, "rev-parse", "HEAD"))
	remoteHead := strings.TrimSpace(runGit(t, remote, "rev-parse", "HEAD"))
	if before == after {
		t.Fatal("Pull() did not advance HEAD")
	}
	if after != remoteHead {
		t.Fatalf("Pull() HEAD = %q, want remote HEAD %q", after, remoteHead)
	}
	content, err := os.ReadFile(filepath.Join(local, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "fast forward\n" {
		t.Fatalf("pulled shared.txt = %q, want %q", content, "fast forward\\n")
	}
}

func TestPullRejectsNonFastForward(t *testing.T) {
	temp := t.TempDir()
	remote := filepath.Join(temp, "remote.git")
	runGit(t, temp, "init", "--bare", remote)

	seed := filepath.Join(temp, "seed")
	runGit(t, temp, "clone", remote, seed)
	configureIdentity(t, seed)
	writeFile(t, filepath.Join(seed, "file.txt"), "base\n")
	runGit(t, seed, "add", "file.txt")
	runGit(t, seed, "commit", "-m", "base")
	runGit(t, seed, "push", "--set-upstream", "origin", "HEAD")

	local := filepath.Join(temp, "local")
	peer := filepath.Join(temp, "peer")
	if err := Clone(remote, local); err != nil {
		t.Fatal(err)
	}
	if err := Clone(remote, peer); err != nil {
		t.Fatal(err)
	}
	configureIdentity(t, local)
	configureIdentity(t, peer)
	writeFile(t, filepath.Join(local, "local.txt"), "local\n")
	runGit(t, local, "add", "local.txt")
	runGit(t, local, "commit", "-m", "local")
	writeFile(t, filepath.Join(peer, "peer.txt"), "peer\n")
	runGit(t, peer, "add", "peer.txt")
	runGit(t, peer, "commit", "-m", "peer")
	runGit(t, peer, "push")

	err := Pull(local)
	if err == nil || !strings.Contains(err.Error(), "fast-forward") {
		t.Fatalf("Pull() error = %v, want fast-forward error", err)
	}
}

func TestCloneErrorRedactsCredentials(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "clone")
	err := Clone("https://secret-user:secret-pass@127.0.0.1:1/repo.git", dest)
	if err == nil {
		t.Fatal("Clone() error = nil, want failure")
	}
	if strings.Contains(err.Error(), "secret-user") || strings.Contains(err.Error(), "secret-pass") {
		t.Fatalf("Clone() exposed credentials: %v", err)
	}
}

func configureIdentity(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "config", "user.name", "Gitenv Test")
	runGit(t, root, "config", "user.email", "gitenv@example.test")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func TestCommitAndPushRequiresMessage(t *testing.T) {
	err := CommitAndPush(t.TempDir(), " \t")
	if !errors.Is(err, errors.New("commit message must not be empty")) && (err == nil || err.Error() != "commit message must not be empty") {
		t.Fatalf("CommitAndPush() error = %v", err)
	}
}
