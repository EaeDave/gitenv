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

func TestReadProfileVerifiesChecksum(t *testing.T) {
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
	original := []byte("SECRET=preserved\r\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LocalConfig{VaultPath: vaultDir, Projects: map[string]LocalProject{}}
	if err := Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	plaintext, err := ReadProfile(&cfg, "api", "dev")
	if err != nil || string(plaintext) != string(original) {
		t.Fatalf("authenticated profile read failed: plaintext=%q err=%v", plaintext, err)
	}
	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	profile := manifest.Projects["api"].Profiles["dev"]
	profile.Checksum = Checksum([]byte("different"))
	project := manifest.Projects["api"]
	project.Profiles["dev"] = profile
	manifest.Projects["api"] = project
	if err := SaveManifest(vaultDir, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadProfile(&cfg, "api", "dev"); err == nil {
		t.Fatal("profile with mismatched checksum was accepted")
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

func TestProfileStatusesMarksOnlyActiveProfileModified(t *testing.T) {
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	vaultDir := filepath.Join(root, "vault")
	if err := Init(vaultDir, identity.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	devEnv := []byte("API_URL=https://dev\n")
	prodEnv := []byte("API_URL=https://prod\n")
	cfg := LocalConfig{VaultPath: vaultDir, Projects: map[string]LocalProject{}}
	if err := Link(&cfg, "api", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), devEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Capture(&cfg, "api", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), prodEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Capture(&cfg, "api", "prod"); err != nil {
		t.Fatal(err)
	}
	if err := Apply(&cfg, "api", "prod", true); err != nil {
		t.Fatal(err)
	}

	// Active profile clean: local .env matches prod snapshot.
	statuses, err := ProfileStatuses(cfg, "api")
	if err != nil {
		t.Fatal(err)
	}
	if statuses["prod"] != "clean" || statuses["dev"] != "" {
		t.Fatalf("clean state = %#v", statuses)
	}

	// Uncaptured local edit: only the active profile turns modified.
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_URL=https://edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	statuses, err = ProfileStatuses(cfg, "api")
	if err != nil {
		t.Fatal(err)
	}
	if statuses["prod"] != "modified" || statuses["dev"] != "" {
		t.Fatalf("modified state = %#v", statuses)
	}

	// Local .env restored to the dev snapshot while prod is active: dev is
	// flagged as the current on-disk match, prod as modified.
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), devEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	statuses, err = ProfileStatuses(cfg, "api")
	if err != nil {
		t.Fatal(err)
	}
	if statuses["dev"] != "current" || statuses["prod"] != "modified" {
		t.Fatalf("current-match state = %#v", statuses)
	}

	// Missing .env: the active profile reports missing.
	if err := os.Remove(filepath.Join(projectDir, ".env")); err != nil {
		t.Fatal(err)
	}
	statuses, err = ProfileStatuses(cfg, "api")
	if err != nil {
		t.Fatal(err)
	}
	if statuses["prod"] != "missing" || statuses["dev"] != "" {
		t.Fatalf("missing state = %#v", statuses)
	}
}
