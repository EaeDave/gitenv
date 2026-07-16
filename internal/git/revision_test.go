package git

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRevisionFilesAndWorktreeFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	runGit(t, t.TempDir(), "init", root)
	configureIdentity(t, root)

	oldBinary := []byte{'o', 'l', 'd', 0, 0xff, '\n'}
	writeBytes(t, filepath.Join(root, "binary.dat"), oldBinary)
	writeFile(t, filepath.Join(root, "name with spaces.txt"), "old spaced content\n")
	writeFile(t, filepath.Join(root, "deleted.txt"), "committed then deleted\n")
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "old revision")
	oldRevision := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	writeBytes(t, filepath.Join(root, "binary.dat"), []byte{'n', 'e', 'w', 0, 0xfe, '\n'})
	writeFile(t, filepath.Join(root, "name with spaces.txt"), "worktree spaced content\n")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "untracked.txt"), "visible untracked\n")
	writeFile(t, filepath.Join(root, ".gitignore"), "ignored.txt\n")
	writeFile(t, filepath.Join(root, "ignored.txt"), "must stay hidden\n")

	beforeStatus := runGit(t, root, "status", "--porcelain=v1", "-z")
	revisionFiles, err := RevisionFiles(root, oldRevision)
	if err != nil {
		t.Fatalf("RevisionFiles() error = %v", err)
	}
	worktreeFiles, err := WorktreeFiles(root)
	if err != nil {
		t.Fatalf("WorktreeFiles() error = %v", err)
	}
	afterStatus := runGit(t, root, "status", "--porcelain=v1", "-z")

	wantRevision := map[string][]byte{
		"binary.dat":           oldBinary,
		"deleted.txt":          []byte("committed then deleted\n"),
		"name with spaces.txt": []byte("old spaced content\n"),
	}
	if !reflect.DeepEqual(revisionFiles, wantRevision) {
		t.Fatalf("RevisionFiles() = %#v, want %#v", revisionFiles, wantRevision)
	}
	wantWorktree := map[string][]byte{
		".gitignore":           []byte("ignored.txt\n"),
		"binary.dat":           {'n', 'e', 'w', 0, 0xfe, '\n'},
		"name with spaces.txt": []byte("worktree spaced content\n"),
		"untracked.txt":        []byte("visible untracked\n"),
	}
	if !reflect.DeepEqual(worktreeFiles, wantWorktree) {
		t.Fatalf("WorktreeFiles() = %#v, want %#v", worktreeFiles, wantWorktree)
	}
	if _, found := worktreeFiles["ignored.txt"]; found {
		t.Fatal("WorktreeFiles() included ignored file")
	}
	if _, found := worktreeFiles["deleted.txt"]; found {
		t.Fatal("WorktreeFiles() included deleted file")
	}
	if beforeStatus != afterStatus {
		t.Fatalf("snapshot reads changed worktree status: before %q, after %q", beforeStatus, afterStatus)
	}
	currentBinary, err := os.ReadFile(filepath.Join(root, "binary.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(currentBinary, wantWorktree["binary.dat"]) {
		t.Fatalf("snapshot reads changed binary.dat to %v", currentBinary)
	}
}

func TestUpstreamRevisionAndHasHead(t *testing.T) {
	temp := t.TempDir()
	remote := filepath.Join(temp, "remote.git")
	runGit(t, temp, "init", "--bare", remote)
	root := filepath.Join(temp, "repository")
	runGit(t, temp, "init", root)
	configureIdentity(t, root)

	if HasHead(root) {
		t.Fatal("HasHead() = true for repository without commits")
	}
	writeFile(t, filepath.Join(root, "file.txt"), "content\n")
	runGit(t, root, "add", "file.txt")
	runGit(t, root, "commit", "-m", "initial")
	if !HasHead(root) {
		t.Fatal("HasHead() = false for repository with commit")
	}
	runGit(t, root, "remote", "add", "origin", remote)
	runGit(t, root, "push", "--set-upstream", "origin", "HEAD")

	got, err := UpstreamRevision(root)
	if err != nil {
		t.Fatalf("UpstreamRevision() error = %v", err)
	}
	want := strings.TrimSpace(runGit(t, root, "rev-parse", "@{upstream}"))
	if got != want {
		t.Fatalf("UpstreamRevision() = %q, want %q", got, want)
	}
}

func writeBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
