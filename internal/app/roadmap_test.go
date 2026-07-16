package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eaedave/gitenv/internal/vault"
)

func TestProtectedVaultUnlocksWithMasterPassword(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	if err := CreateProtectedVault(&cfg, filepath.Join(root, "vault"), "correct horse battery staple", "correct horse battery staple", "", "test-device", vault.StoreIdentitySession); err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != vault.ManifestVersion || manifest.WrappedIdentity == nil {
		t.Fatalf("vault not protected: %#v", manifest)
	}
	vault.ClearSessionIdentity()
	if err := vault.UnlockVault(cfg.VaultPath, "wrong", vault.StoreIdentitySession); err == nil {
		t.Fatal("wrong password unlocked vault")
	}
	if err := vault.UnlockVault(cfg.VaultPath, "correct horse battery staple", vault.StoreIdentitySession); err != nil {
		t.Fatal(err)
	}
	recoveryPath := filepath.Join(root, "recovery.txt")
	if err := ExportIdentity(recoveryPath); err != nil {
		t.Fatal(err)
	}
	recovery, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.ParseIdentity(recovery); err != nil {
		t.Fatalf("invalid exported recovery: %v", err)
	}
}

func TestProtectedVaultRejectsWeakPasswordBeforeCreatingFiles(t *testing.T) {
	root := t.TempDir()
	vaultPath := filepath.Join(root, "vault")
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	if err := CreateProtectedVault(&cfg, vaultPath, "short", "short", "", "device", vault.StoreIdentitySession); err == nil {
		t.Fatal("weak password accepted")
	}
	if _, err := os.Stat(vaultPath); !os.IsNotExist(err) {
		t.Fatalf("weak password left vault files behind: %v", err)
	}
}

func TestMigrateVaultAccessPreservesCiphertext(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	if err := CreateVault(&cfg, filepath.Join(root, "vault"), "", ""); err != nil {
		t.Fatal(err)
	}
	current, err := DetectCurrent(cfg, project)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddCurrentProject(&cfg, current, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	profilePath := vault.ProfilePath(cfg.VaultPath, "api", "dev")
	before, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = 1
	manifest.WrappedIdentity = nil
	if err := vault.SaveManifest(cfg.VaultPath, manifest); err != nil {
		t.Fatal(err)
	}
	if err := MigrateVaultAccess(cfg, "migration-password", "migration-password", "legacy-device", vault.StoreIdentitySession); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("migration rewrote profile ciphertext")
	}
}

func TestProjectRepositoryIdentityIsPortableAndPathStaysLocal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	project := filepath.Join(root, "linux-path")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{}}
	if err := CreateVault(&cfg, filepath.Join(root, "vault"), "", ""); err != nil {
		t.Fatal(err)
	}
	current := CurrentProject{Path: project, Name: "api", HasEnv: true, RepositoryIdentity: "github.com/eaedave/api"}
	if err := AddCurrentProject(&cfg, current, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := MatchVaultProject(manifest, CurrentProject{RepositoryIdentity: "github.com/eaedave/api"}); got != "api" {
		t.Fatalf("match = %q", got)
	}
	if len(manifest.Projects["api"].Repositories) != 1 {
		t.Fatalf("repository identity missing: %#v", manifest.Projects["api"])
	}
	data, err := os.ReadFile(filepath.Join(cfg.VaultPath, "gitenv.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stringContains(string(data), project) {
		t.Fatal("local path leaked into vault manifest")
	}
}

func stringContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
