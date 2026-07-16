package app

import (
	"os"
	"path/filepath"
	"testing"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func TestPublishLocalEnvCapturesSelectedProfileAndPushesIt(t *testing.T) {
	cfg, projectDir := newSyncedEnvironment(t)
	updated := []byte("API_KEY=new-secret\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), updated, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := PublishLocalEnv(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	got, err := vault.ReadProfile(&cfg, "api", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(updated) {
		t.Fatalf("published profile = %q, want %q", got, updated)
	}
	if status := gitops.InspectSync(cfg.VaultPath); status.State != gitops.SyncSynced || status.Dirty {
		t.Fatalf("post-publish status = %#v", status)
	}
}

func TestPublishLocalEnvRejectsDirtyVaultBeforeCapture(t *testing.T) {
	cfg, projectDir := newSyncedEnvironment(t)
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_KEY=must-not-capture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.VaultPath, "dirty-marker"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PublishLocalEnv(&cfg, "api", "dev"); err == nil {
		t.Fatal("dirty vault publish unexpectedly succeeded")
	}
	got, err := vault.ReadProfile(&cfg, "api", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "API_KEY=old-secret\n" {
		t.Fatalf("guard captured local env before rejecting: %q", got)
	}
}

func TestDiscardLocalEnvRestoresActiveProfile(t *testing.T) {
	cfg, projectDir := newSyncedEnvironment(t)
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_KEY=discard-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DiscardLocalEnv(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "API_KEY=old-secret\n" {
		t.Fatalf("restored .env = %q", got)
	}
}

func newSyncedEnvironment(t *testing.T) (vault.LocalConfig, string) {
	t.Helper()
	cfg, root := newVaultForRemote(t)
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_KEY=old-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vault.Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(root, "remote.git")
	initBareRepo(t, bare)
	if err := ConfigureVaultRemote(cfg, bare); err != nil {
		t.Fatal(err)
	}
	commitVault(t, cfg.VaultPath, "initial")
	if err := gitops.Push(cfg.VaultPath); err != nil {
		t.Fatal(err)
	}
	return cfg, projectDir
}
