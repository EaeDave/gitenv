package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetMissingVaultOnlyMutatesSessionCopy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	missing := filepath.Join(root, "deleted-vault")
	original := LocalConfig{VaultPath: missing, Projects: map[string]LocalProject{"api": {Path: "/api"}}}
	if err := SaveLocal(original); err != nil {
		t.Fatal(err)
	}
	session, err := LoadLocal()
	if err != nil {
		t.Fatal(err)
	}
	reset, err := ResetMissingVault(&session)
	if err != nil {
		t.Fatal(err)
	}
	if !reset || session.VaultPath != "" || len(session.Projects) != 0 {
		t.Fatalf("session not reset: %#v", session)
	}
	persisted, err := LoadLocal()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.VaultPath != missing || len(persisted.Projects) != 1 {
		t.Fatalf("persistent config was changed: %#v", persisted)
	}
}

func TestResetMissingVaultKeepsExistingManifest(t *testing.T) {
	root := t.TempDir()
	vaultDir := filepath.Join(root, "vault")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, manifestName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LocalConfig{VaultPath: vaultDir, Projects: map[string]LocalProject{}}
	reset, err := ResetMissingVault(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reset || cfg.VaultPath != vaultDir {
		t.Fatalf("existing vault reset: %#v", cfg)
	}
}
