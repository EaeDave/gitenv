package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectSyncStates(t *testing.T) {
	t.Run("no remote", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "vault")
		if err := Init(root); err != nil {
			t.Fatal(err)
		}
		status := InspectSync(root)
		if status.State != SyncNoRemote {
			t.Fatalf("state = %q, want %q", status.State, SyncNoRemote)
		}
	})

	t.Run("synced and dirty", func(t *testing.T) {
		local, _ := setupSyncRepositories(t)
		status := InspectSync(local)
		if status.State != SyncSynced || status.Ahead != 0 || status.Behind != 0 || status.Dirty {
			t.Fatalf("synced status = %#v", status)
		}
		writeFile(t, filepath.Join(local, "local.txt"), "pending\n")
		status = InspectSync(local)
		if status.State != SyncLocalAhead || !status.Dirty {
			t.Fatalf("dirty status = %#v", status)
		}
	})

	t.Run("remote ahead", func(t *testing.T) {
		local, peer := setupSyncRepositories(t)
		commitAndPushFile(t, peer, "remote.txt", "remote\n", "remote update")
		status := InspectSync(local)
		if status.State != SyncRemoteAhead || status.Ahead != 0 || status.Behind != 1 {
			t.Fatalf("remote-ahead status = %#v", status)
		}
	})

	t.Run("local ahead", func(t *testing.T) {
		local, _ := setupSyncRepositories(t)
		commitFile(t, local, "local.txt", "local\n", "local update")
		status := InspectSync(local)
		if status.State != SyncLocalAhead || status.Ahead != 1 || status.Behind != 0 {
			t.Fatalf("local-ahead status = %#v", status)
		}
	})

	t.Run("diverged", func(t *testing.T) {
		local, peer := setupSyncRepositories(t)
		commitFile(t, local, "local.txt", "local\n", "local update")
		commitAndPushFile(t, peer, "remote.txt", "remote\n", "remote update")
		status := InspectSync(local)
		if status.State != SyncDiverged || status.Ahead != 1 || status.Behind != 1 {
			t.Fatalf("diverged status = %#v", status)
		}
	})

	t.Run("dirty and remote ahead blocks", func(t *testing.T) {
		local, peer := setupSyncRepositories(t)
		writeFile(t, filepath.Join(local, "pending.txt"), "pending\n")
		commitAndPushFile(t, peer, "remote.txt", "remote\n", "remote update")
		status := InspectSync(local)
		if status.State != SyncDiverged || !status.Dirty || status.Behind != 1 {
			t.Fatalf("dirty-diverged status = %#v", status)
		}
	})
}

func TestPushPublishesExistingCommitWithoutTouchingWorktree(t *testing.T) {
	local, peer := setupSyncRepositories(t)
	commitFile(t, local, "committed.txt", "committed\n", "local commit")
	writeFile(t, filepath.Join(local, "pending.txt"), "pending\n")
	if err := Push(local); err != nil {
		t.Fatal(err)
	}
	if got := InspectSync(local); got.State != SyncLocalAhead || !got.Dirty || got.Ahead != 0 {
		t.Fatalf("push should preserve uncommitted worktree changes: %#v", got)
	}
	if err := Pull(peer); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(peer, "committed.txt")); err != nil {
		t.Fatalf("existing commit was not published: %v", err)
	}
	if _, err := os.Stat(filepath.Join(peer, "pending.txt")); !os.IsNotExist(err) {
		t.Fatalf("uncommitted file was unexpectedly published: %v", err)
	}
}

func TestClassifyRemoteErrorTreatsTimeoutAsOffline(t *testing.T) {
	state, detail := classifyRemoteError(context.DeadlineExceeded)
	if state != SyncOffline || detail != "remote check timed out" {
		t.Fatalf("timeout classification = %q, %q", state, detail)
	}
}

func setupSyncRepositories(t *testing.T) (string, string) {
	t.Helper()
	temp := t.TempDir()
	remote := filepath.Join(temp, "remote.git")
	runGit(t, temp, "init", "--bare", remote)
	seed := filepath.Join(temp, "seed")
	runGit(t, temp, "clone", remote, seed)
	configureIdentity(t, seed)
	commitAndPushFile(t, seed, "manifest.json", "{}\n", "initial")
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
	return local, peer
}

func commitAndPushFile(t *testing.T, root, name, content, message string) {
	t.Helper()
	commitFile(t, root, name, content, message)
	runGit(t, root, "push")
}

func commitFile(t *testing.T, root, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", name)
	runGit(t, root, "commit", "-m", message)
}
