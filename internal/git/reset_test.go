package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRefCreatesReachableRefAndRefusesToClobber(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	configureIdentity(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "one\n")
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-m", "one")
	first := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	name1, err := BackupRef(root, "gitenv-backup")
	if err != nil {
		t.Fatalf("BackupRef() error = %v", err)
	}
	if !strings.HasPrefix(name1, "gitenv-backup/") {
		t.Fatalf("backup ref name = %q, want gitenv-backup/ prefix", name1)
	}
	if got := strings.TrimSpace(runGit(t, root, "rev-parse", name1)); got != first {
		t.Fatalf("backup ref points at %q, want %q", got, first)
	}

	// Advance HEAD, then back up again. name1 already occupies the base name for
	// the current second, so this second backup must not clobber it: it lands on
	// a distinct name and name1 keeps pointing at the original commit.
	writeFile(t, filepath.Join(root, "a.txt"), "two\n")
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-m", "two")
	second := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	name2, err := BackupRef(root, "gitenv-backup")
	if err != nil {
		t.Fatalf("second BackupRef() error = %v", err)
	}
	if name2 == name1 {
		t.Fatalf("BackupRef reused the existing ref name %q", name1)
	}
	if got := strings.TrimSpace(runGit(t, root, "rev-parse", name2)); got != second {
		t.Fatalf("second backup ref points at %q, want %q", got, second)
	}
	if got := strings.TrimSpace(runGit(t, root, "rev-parse", name1)); got != first {
		t.Fatalf("existing ref was moved to %q, want %q", got, first)
	}
}

func TestResetHardMovesHeadAndWorktree(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	configureIdentity(t, root)
	envPath := filepath.Join(root, "a.txt")
	writeFile(t, envPath, "one\n")
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-m", "one")
	original := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	writeFile(t, envPath, "two\n")
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-m", "two")

	if err := ResetHard(root, original); err != nil {
		t.Fatalf("ResetHard() error = %v", err)
	}
	if got := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD")); got != original {
		t.Fatalf("HEAD = %q after reset, want %q", got, original)
	}
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "one\n" {
		t.Fatalf("worktree file = %q after reset, want %q", content, "one\n")
	}
}

func TestCommitAllWorksWithoutGlobalGitIdentity(t *testing.T) {
	// Strip every source of a git identity: no system config, an empty global
	// config, and a HOME with no .gitconfig. A plain `git commit` would abort
	// here; CommitAll must still succeed because it supplies its own identity.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "a.txt"), "value\n")

	if err := CommitAll(root, "seed"); err != nil {
		t.Fatalf("CommitAll() error = %v", err)
	}
	if !HasHead(root) {
		t.Fatal("CommitAll() produced no commit")
	}
	if author := strings.TrimSpace(runGit(t, root, "log", "-1", "--format=%an <%ae>")); author != "gitenv <gitenv@localhost>" {
		t.Fatalf("commit author = %q, want gitenv <gitenv@localhost>", author)
	}

	// A second CommitAll with nothing staged is a no-op, not an error.
	if err := CommitAll(root, "empty"); err != nil {
		t.Fatalf("empty CommitAll() error = %v, want nil", err)
	}
	if count := strings.TrimSpace(runGit(t, root, "rev-list", "--count", "HEAD")); count != "1" {
		t.Fatalf("commit count = %q, want 1 (empty commit created)", count)
	}
	if err := CommitAll(root, "  "); err == nil {
		t.Fatal("CommitAll() with blank message = nil, want error")
	}
}
