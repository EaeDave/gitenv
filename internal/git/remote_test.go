package git

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAddRemoteAndReadURL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	url := "git@example.test:owner/vault.git"
	if err := AddRemote(root, "origin", url); err != nil {
		t.Fatal(err)
	}
	got, err := RemoteURL(root, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if got != url {
		t.Fatalf("RemoteURL() = %q, want %q", got, url)
	}
	if !HasRemote(root, "origin") {
		t.Fatal("HasRemote returned false")
	}
}

func TestSetRemoteURL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if err := AddRemote(root, "origin", "git@example.test:owner/vault.git"); err != nil {
		t.Fatal(err)
	}

	updated := "git@example.test:owner/renamed.git"
	if err := SetRemoteURL(root, "origin", updated); err != nil {
		t.Fatalf("SetRemoteURL() error = %v", err)
	}
	got, err := RemoteURL(root, "origin")
	if err != nil {
		t.Fatalf("RemoteURL() after SetRemoteURL() error = %v", err)
	}
	if got != updated {
		t.Fatalf("RemoteURL() after SetRemoteURL() = %q, want %q", got, updated)
	}
}

func TestSetRemoteURLRequiresNameAndURL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if err := SetRemoteURL(root, "", "https://example.test/repo.git"); err == nil {
		t.Fatal("SetRemoteURL() with empty name expected error, got nil")
	}
	if err := SetRemoteURL(root, "origin", ""); err == nil {
		t.Fatal("SetRemoteURL() with empty URL expected error, got nil")
	}
}

func TestRemoveRemote(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if err := AddRemote(root, "origin", "git@example.test:owner/vault.git"); err != nil {
		t.Fatal(err)
	}
	if !HasRemote(root, "origin") {
		t.Fatal("expected origin to exist before removal")
	}

	if err := RemoveRemote(root, "origin"); err != nil {
		t.Fatalf("RemoveRemote() error = %v", err)
	}
	if HasRemote(root, "origin") {
		t.Fatal("HasRemote returned true after RemoveRemote")
	}
}

func TestRemoveRemoteRequiresName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemote(root, ""); err == nil {
		t.Fatal("RemoveRemote() with empty name expected error, got nil")
	}
}

func TestPingRemote(t *testing.T) {
	temp := t.TempDir()
	bare := filepath.Join(temp, "bare.git")
	runGit(t, temp, "init", "--bare", bare)

	repo := filepath.Join(temp, "repo")
	if err := Init(repo); err != nil {
		t.Fatal(err)
	}
	if err := AddRemote(repo, "origin", bare); err != nil {
		t.Fatal(err)
	}

	t.Run("reachable", func(t *testing.T) {
		if err := PingRemote(repo, "origin"); err != nil {
			t.Fatalf("PingRemote() on reachable remote error = %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		if err := SetRemoteURL(repo, "origin", filepath.Join(temp, "does-not-exist.git")); err != nil {
			t.Fatal(err)
		}
		if err := PingRemote(repo, "origin"); err == nil {
			t.Fatal("PingRemote() on unreachable remote expected error, got nil")
		}
	})

	t.Run("redacts_credentials", func(t *testing.T) {
		// Restore a URL with embedded credentials that will fail to connect.
		// Port 1 on loopback is reliably refused; credential redaction must apply.
		if err := SetRemoteURL(repo, "origin", "https://secret-user:secret-pass@127.0.0.1:1/repo.git"); err != nil {
			t.Fatal(err)
		}
		err := PingRemote(repo, "origin")
		if err == nil {
			t.Fatal("PingRemote() with bad URL expected error, got nil")
		}
		if strings.Contains(err.Error(), "secret-user") || strings.Contains(err.Error(), "secret-pass") {
			t.Fatalf("PingRemote() exposed credentials: %v", err)
		}
	})
}

func TestPingRemoteRequiresName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if err := PingRemote(root, ""); err == nil {
		t.Fatal("PingRemote() with empty name expected error, got nil")
	}
}
