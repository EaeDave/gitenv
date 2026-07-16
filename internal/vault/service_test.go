package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureApplyPreservesBytesAndProtectsChanges(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	t.Setenv("GITENV_CONFIG_DIR", configDir)
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	vaultDir := filepath.Join(root, "vault")
	if err := Init(vaultDir, identity.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# comment\r\nAPI_URL=https://dev\r\n#API_URL=https://prod\r\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LocalConfig{VaultPath: vaultDir, Projects: map[string]LocalProject{}}
	if err := Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := SaveLocal(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(&cfg, "api", "dev", false); err == nil {
		t.Fatal("expected modified file protection")
	}
	if err := Apply(&cfg, "api", "dev", true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("bytes changed: %q", got)
	}
}

func TestRemoveProfileRejectsActiveAndRemovesInactive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	vaultDir, projectDir := filepath.Join(root, "vault"), filepath.Join(root, "project")
	if err := Init(vaultDir, identity.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LocalConfig{VaultPath: vaultDir, Projects: map[string]LocalProject{}}
	if err := Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := Capture(&cfg, "api", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(&cfg, "api", "prod"); err == nil {
		t.Fatal("active profile removal succeeded")
	}
	if err := Apply(&cfg, "api", "dev", false); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(&cfg, "api", "prod"); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := manifest.Projects["api"].Profiles["prod"]; exists {
		t.Fatal("profile remains in manifest")
	}
	if _, err := os.Stat(ProfilePath(vaultDir, "api", "prod")); !os.IsNotExist(err) {
		t.Fatalf("profile file still exists: %v", err)
	}
}

func TestLoadIdentityForManifestSkipsStaleSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	allowed, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentityFallback(allowed); err != nil {
		t.Fatal(err)
	}
	stale, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentityToKeychain(stale); err != nil {
		t.Fatal(err)
	}
	SetSessionIdentity(stale)
	t.Cleanup(ClearSessionIdentity)
	manifest := Manifest{Recipients: []string{allowed.Recipient().String()}}
	got, err := LoadIdentityForManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Recipient().String() != allowed.Recipient().String() {
		t.Fatalf("selected recipient = %s, want %s", got.Recipient(), allowed.Recipient())
	}
	cached, err := LoadIdentity()
	if err != nil || cached.Recipient().String() != allowed.Recipient().String() {
		t.Fatalf("authorized fallback was not cached: identity=%v err=%v", cached, err)
	}
}
