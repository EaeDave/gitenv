package app

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eaedave/gitenv/internal/vault"
)

// newVaultForRemote creates an isolated vault with a fresh git repository.
func newVaultForRemote(t *testing.T) (vault.LocalConfig, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	recovery := filepath.Join(root, "recovery.txt")
	if err := CreateVault(&cfg, filepath.Join(root, "vault"), recovery, ""); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	return cfg, root
}

// initBareRepo initialises a bare git repository at path for use as a local remote.
func initBareRepo(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command("git", "init", "--bare", path).CombinedOutput()
	if err != nil {
		t.Fatalf("git init --bare %s: %v\n%s", path, err, out)
	}
}

func TestVaultRemoteURL(t *testing.T) {
	cfg, _ := newVaultForRemote(t)

	// No remote yet — must fail.
	if _, err := VaultRemoteURL(cfg); err == nil {
		t.Fatal("VaultRemoteURL() with no remote expected error, got nil")
	}

	const want = "git@example.test:owner/vault.git"
	if err := ConfigureVaultRemote(cfg, want); err != nil {
		t.Fatalf("ConfigureVaultRemote(): %v", err)
	}
	got, err := VaultRemoteURL(cfg)
	if err != nil {
		t.Fatalf("VaultRemoteURL(): %v", err)
	}
	if got != want {
		t.Fatalf("VaultRemoteURL() = %q, want %q", got, want)
	}
}

func TestConfigureVaultRemote(t *testing.T) {
	cfg, _ := newVaultForRemote(t)

	// First call adds the remote.
	first := "git@example.test:owner/vault.git"
	if err := ConfigureVaultRemote(cfg, first); err != nil {
		t.Fatalf("ConfigureVaultRemote() add: %v", err)
	}
	got, err := VaultRemoteURL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != first {
		t.Fatalf("after add: URL = %q, want %q", got, first)
	}

	// Second call updates the existing remote.
	second := "git@example.test:owner/renamed.git"
	if err := ConfigureVaultRemote(cfg, second); err != nil {
		t.Fatalf("ConfigureVaultRemote() set: %v", err)
	}
	got, err = VaultRemoteURL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != second {
		t.Fatalf("after set: URL = %q, want %q", got, second)
	}
}

func TestRemoveVaultRemote(t *testing.T) {
	cfg, _ := newVaultForRemote(t)

	if err := ConfigureVaultRemote(cfg, "git@example.test:owner/vault.git"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveVaultRemote(cfg); err != nil {
		t.Fatalf("RemoveVaultRemote(): %v", err)
	}
	if _, err := VaultRemoteURL(cfg); err == nil {
		t.Fatal("VaultRemoteURL() after RemoveVaultRemote() expected error, got nil")
	}
}

func TestTestVaultRemote(t *testing.T) {
	cfg, root := newVaultForRemote(t)

	bare := filepath.Join(root, "bare.git")
	initBareRepo(t, bare)

	if err := ConfigureVaultRemote(cfg, bare); err != nil {
		t.Fatal(err)
	}

	t.Run("reachable", func(t *testing.T) {
		if err := TestVaultRemote(cfg); err != nil {
			t.Fatalf("TestVaultRemote() on reachable remote: %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		if err := ConfigureVaultRemote(cfg, filepath.Join(root, "nonexistent.git")); err != nil {
			t.Fatal(err)
		}
		if err := TestVaultRemote(cfg); err == nil {
			t.Fatal("TestVaultRemote() on unreachable remote expected error, got nil")
		}
	})
}

func TestVaultRemoteNoVault(t *testing.T) {
	cfg := vault.LocalConfig{}
	if _, err := VaultRemoteURL(cfg); err == nil {
		t.Fatal("VaultRemoteURL() with no vault expected error")
	}
	if err := ConfigureVaultRemote(cfg, "https://example.test/repo.git"); err == nil {
		t.Fatal("ConfigureVaultRemote() with no vault expected error")
	}
	if err := RemoveVaultRemote(cfg); err == nil {
		t.Fatal("RemoveVaultRemote() with no vault expected error")
	}
	if err := TestVaultRemote(cfg); err == nil {
		t.Fatal("TestVaultRemote() with no vault expected error")
	}
}
