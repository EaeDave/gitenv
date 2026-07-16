package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eaedave/gitenv/internal/vault"
)

func TestCreateDetectAndAddCurrentProject(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	project := filepath.Join(root, "my-api")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# comment\r\nTOKEN=secret\r\n")
	if err := os.WriteFile(filepath.Join(project, ".env"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	recovery := filepath.Join(root, "recovery.txt")
	if err := CreateVault(&cfg, filepath.Join(root, "vault"), recovery, ""); err != nil {
		t.Fatal(err)
	}
	current, err := DetectCurrent(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	if !current.HasEnv || current.Name != "my-api" || current.LinkedName != "" {
		t.Fatalf("unexpected detection: %#v", current)
	}
	if err := AddCurrentProject(&cfg, current, "my-api", "dev"); err != nil {
		t.Fatal(err)
	}
	current, err = DetectCurrent(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	if current.LinkedName != "my-api" {
		t.Fatalf("linked name = %q", current.LinkedName)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	profile := manifest.Projects["my-api"].Profiles["dev"]
	if profile.Checksum != vault.Checksum(original) {
		t.Fatal("captured profile checksum mismatch")
	}
	if _, err := os.Stat(recovery); err != nil {
		t.Fatalf("recovery not exported: %v", err)
	}
}

func TestLinkExistingProjectAppliesVaultProfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	vaultEnv := []byte("FROM_VAULT=trusted\n")
	if err := os.WriteFile(filepath.Join(source, ".env"), vaultEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".env"), []byte("LOCAL=discard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	if err := CreateVault(&cfg, filepath.Join(root, "vault"), filepath.Join(root, "recovery"), ""); err != nil {
		t.Fatal(err)
	}
	sourceCurrent, err := DetectCurrent(cfg, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddCurrentProject(&cfg, sourceCurrent, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	delete(cfg.Projects, "api")
	if err := vault.SaveLocal(cfg); err != nil {
		t.Fatal(err)
	}
	targetCurrent, err := DetectCurrent(cfg, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := LinkExistingProject(&cfg, targetCurrent, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(vaultEnv) {
		t.Fatalf("target env = %q, want vault profile", got)
	}
}

func TestCloneVaultImportsRecovery(t *testing.T) {
	root := t.TempDir()
	firstConfig := filepath.Join(root, "first-config")
	t.Setenv("GITENV_CONFIG_DIR", firstConfig)
	first := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	recovery := filepath.Join(root, "recovery.txt")
	if err := CreateVault(&first, filepath.Join(root, "source"), recovery, ""); err != nil {
		t.Fatal(err)
	}
	// Clone requires a committed repository; Git integration covers remote transport.
	if err := os.Remove(filepath.Join(firstConfig, "identity.txt")); err != nil {
		t.Fatal(err)
	}
	if err := ImportIdentity(recovery); err != nil {
		t.Fatal(err)
	}
	identity, err := vault.LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(first.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Recipient().String() != manifest.Recipients[0] {
		t.Fatal("imported identity does not unlock vault recipient")
	}
}

func TestAddCurrentRejectsDirectoryWithoutEnv(t *testing.T) {
	root := t.TempDir()
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	current, err := DetectCurrent(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddCurrentProject(&cfg, current, "empty", "dev"); err == nil {
		t.Fatal("expected missing .env error")
	}
}
