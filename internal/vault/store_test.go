package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"
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

func TestSaveManifestOmitsProjectMetadataFromBootstrap(t *testing.T) {
	identity, vaultDir := newStoreVault(t)
	manifest := Manifest{
		Version:    ManifestVersion,
		Recipients: []string{identity.Recipient().String()},
		Projects:   map[string]Project{},
	}
	if _, err := manifest.EnsureProfileID("billing", "staging"); err != nil {
		t.Fatal(err)
	}
	entry := manifest.Projects["billing"]
	entry.Repositories = []Repository{{Identity: "github.com/acme/billing", CloneURL: "https://github.com/acme/billing.git"}}
	entry.EnvFile = "config/.env.secret"
	entry.LineEndings = LineEndingsCRLF
	manifest.Projects["billing"] = entry

	if err := SaveManifest(vaultDir, manifest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(vaultDir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"billing", "staging", "github.com/acme/billing", "config/.env.secret", "crlf"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("gitenv.json leaked %q: %s", secret, data)
		}
	}
}

func TestManifestRoundTripPreservesProjects(t *testing.T) {
	identity, vaultDir := newStoreVault(t)
	stamp := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	manifest := Manifest{
		Version:    ManifestVersion,
		Recipients: []string{identity.Recipient().String()},
		Projects:   map[string]Project{},
	}
	if _, err := manifest.EnsureProfileID("api", "dev"); err != nil {
		t.Fatal(err)
	}
	api := manifest.Projects["api"]
	api.Repositories = []Repository{{Identity: "github.com/acme/api"}}
	api.EnvFile = "services/api/.env"
	api.LineEndings = LineEndingsLF
	dev := api.Profiles["dev"]
	dev.UpdatedAt = stamp
	dev.Checksum = Checksum([]byte("API_KEY=1\n"))
	api.Profiles["dev"] = dev
	manifest.Projects["api"] = api
	if _, err := manifest.EnsureProjectID("web"); err != nil {
		t.Fatal(err)
	}

	if err := SaveManifest(vaultDir, manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != len(manifest.Projects) {
		t.Fatalf("project count: got %d want %d", len(loaded.Projects), len(manifest.Projects))
	}
	for name, want := range manifest.Projects {
		got, ok := loaded.Projects[name]
		if !ok {
			t.Fatalf("project %q missing after reload", name)
		}
		wantBytes, err := marshalProject(want)
		if err != nil {
			t.Fatal(err)
		}
		gotBytes, err := marshalProject(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(wantBytes, gotBytes) {
			t.Fatalf("project %q changed across round trip:\n want %s\n  got %s", name, wantBytes, gotBytes)
		}
	}
}

func TestSaveManifestSkipsUnchangedMetadata(t *testing.T) {
	identity, vaultDir := newStoreVault(t)
	manifest := Manifest{
		Version:    ManifestVersion,
		Recipients: []string{identity.Recipient().String()},
		Projects:   map[string]Project{},
	}
	if _, err := manifest.EnsureProjectID("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.EnsureProjectID("beta"); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(vaultDir, manifest); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	before := readMetaBytes(t, vaultDir)
	if len(before) != 2 {
		t.Fatalf("expected 2 meta.age files, got %d", len(before))
	}
	if err := SaveManifest(vaultDir, loaded); err != nil {
		t.Fatal(err)
	}
	after := readMetaBytes(t, vaultDir)
	if len(after) != len(before) {
		t.Fatalf("meta.age count changed: %d -> %d", len(before), len(after))
	}
	for id, data := range before {
		if !bytes.Equal(after[id], data) {
			t.Fatalf("meta.age %s was rewritten on an unchanged save", id)
		}
	}
}

func TestLoadManifestSealsWhenIdentityUnavailable(t *testing.T) {
	ClearSessionIdentity()
	t.Cleanup(ClearSessionIdentity)
	vaultDir := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", t.TempDir()) // no identity.txt lives here

	// A recipient whose private key is never stored anywhere: nothing can unlock.
	locked, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	writeManifestJSON(t, vaultDir, Manifest{Version: ManifestVersion, Recipients: []string{locked.Recipient().String()}})
	writeProjectMeta(t, vaultDir, []age.Recipient{locked.Recipient()}, Project{Name: "secret", Profiles: map[string]Profile{}})

	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatalf("sealed load must not error: %v", err)
	}
	if !manifest.Sealed {
		t.Fatal("manifest should be sealed when no identity can read metadata")
	}
	if len(manifest.Projects) != 0 {
		t.Fatalf("sealed manifest must expose no projects, got %d", len(manifest.Projects))
	}
	if err := SaveManifest(vaultDir, manifest); err == nil {
		t.Fatal("SaveManifest must refuse a sealed manifest")
	}
}

func TestUpgradeManifestV2ToV3IsIdempotent(t *testing.T) {
	identity, vaultDir := newStoreVault(t)
	plaintext := []byte("SECRET=keepme\nPORT=8080\n")
	stamp := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	v2 := map[string]any{
		"version":    2,
		"recipients": []string{identity.Recipient().String()},
		"projects": map[string]any{
			"myapp": map[string]any{
				"profiles": map[string]any{
					"dev": map[string]any{"updated_at": stamp, "checksum": Checksum(plaintext)},
				},
				"repositories": []string{"github.com/acme/myapp"},
			},
		},
		"custom_key": "preserve-me",
	}
	data, err := json.MarshalIndent(v2, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(vaultDir, manifestName), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := Encrypt(plaintext, []age.Recipient{identity.Recipient()})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(vaultDir, "projects", "myapp", "dev.env.age"), ciphertext, 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := UpgradeManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first upgrade should report a change")
	}

	raw, err := os.ReadFile(filepath.Join(vaultDir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	var bootstrap map[string]any
	if err := json.Unmarshal(raw, &bootstrap); err != nil {
		t.Fatal(err)
	}
	if v, _ := bootstrap["version"].(float64); int(v) != ManifestVersion {
		t.Fatalf("version not upgraded: %v", bootstrap["version"])
	}
	if _, ok := bootstrap["projects"]; ok {
		t.Fatal("v3 gitenv.json must not contain a projects key")
	}
	if bootstrap["custom_key"] != "preserve-me" {
		t.Fatalf("unknown key not preserved: %v", bootstrap["custom_key"])
	}
	if bytes.Contains(raw, []byte("myapp")) {
		t.Fatalf("gitenv.json still leaks project name: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "projects", "myapp")); !os.IsNotExist(err) {
		t.Fatalf("old project directory not removed: %v", err)
	}

	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := manifest.Projects["myapp"]
	if !ok {
		t.Fatal("myapp missing after upgrade")
	}
	if len(entry.Repositories) != 1 || entry.Repositories[0].Identity != "github.com/acme/myapp" {
		t.Fatalf("repositories not migrated: %#v", entry.Repositories)
	}
	prof, ok := entry.Profiles["dev"]
	if !ok {
		t.Fatal("dev profile missing after upgrade")
	}
	if prof.Checksum != Checksum(plaintext) {
		t.Fatalf("profile checksum changed: %s", prof.Checksum)
	}
	path, ok := ProfilePath(vaultDir, manifest, "myapp", "dev")
	if !ok {
		t.Fatal("profile path unresolved after upgrade")
	}
	moved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(moved, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("profile plaintext changed across upgrade: %q", got)
	}

	changed, err = UpgradeManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second upgrade should report no change")
	}
}

// TestUpgradeManifestSurvivesInterruptionBeforeCommit pins the crash-safety
// contract: because ciphertexts are copied rather than moved, a run that dies
// after relocating files but before rewriting gitenv.json leaves a fully valid
// v2 vault, and the next run completes the upgrade and sweeps the residue.
func TestUpgradeManifestSurvivesInterruptionBeforeCommit(t *testing.T) {
	identity, vaultDir := newStoreVault(t)
	plaintext := []byte("SECRET=keepme\nPORT=8080\n")
	writeV2Vault(t, identity, vaultDir, plaintext)

	// Simulate a crash between copying a ciphertext and the commit point: an
	// unreferenced id directory holds a copy while gitenv.json is still v2.
	orphanProject, orphanProfile := "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	v2Ciphertext, err := os.ReadFile(filepath.Join(vaultDir, "projects", "myapp", "dev.env.age"))
	if err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(vaultDir, filepath.FromSlash(profileRelPath(orphanProject, orphanProfile)))
	if err := WriteAtomic(orphanPath, v2Ciphertext, 0o600); err != nil {
		t.Fatal(err)
	}

	// The interrupted state must still be a complete v2 vault: the original
	// ciphertext is intact and decrypts. Moving instead of copying would have
	// destroyed it here.
	original, err := os.ReadFile(filepath.Join(vaultDir, "projects", "myapp", "dev.env.age"))
	if err != nil {
		t.Fatalf("v2 ciphertext lost by interrupted upgrade: %v", err)
	}
	recovered, err := Decrypt(original, identity)
	if err != nil {
		t.Fatalf("v2 ciphertext no longer decrypts: %v", err)
	}
	if !bytes.Equal(recovered, plaintext) {
		t.Fatalf("v2 plaintext changed: %q", recovered)
	}

	changed, err := UpgradeManifest(vaultDir)
	if err != nil {
		t.Fatalf("re-run after interruption failed: %v", err)
	}
	if !changed {
		t.Fatal("re-run after interruption should report a change")
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "projects", orphanProject)); !os.IsNotExist(err) {
		t.Fatalf("unreferenced id directory not swept: %v", err)
	}

	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	path, ok := ProfilePath(vaultDir, manifest, "myapp", "dev")
	if !ok {
		t.Fatal("profile path unresolved after recovered upgrade")
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(stored, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("profile plaintext changed across recovered upgrade: %q", got)
	}
}

// TestUpgradeManifestSweepsV2ResidueAfterCommit covers the other side of the
// commit point: a crash after gitenv.json was rewritten leaves the old
// name-keyed directories behind, and they must not linger as plaintext-named
// evidence of what the vault contains.
func TestUpgradeManifestSweepsV2ResidueAfterCommit(t *testing.T) {
	identity, vaultDir := newStoreVault(t)
	plaintext := []byte("TOKEN=abc\n")
	writeV2Vault(t, identity, vaultDir, plaintext)
	if _, err := UpgradeManifest(vaultDir); err != nil {
		t.Fatal(err)
	}
	// Recreate the residue an interrupted cleanup would have left.
	if err := WriteAtomic(filepath.Join(vaultDir, "projects", "myapp", "dev.env.age"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := UpgradeManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("sweeping v2 residue should report a change")
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "projects", "myapp")); !os.IsNotExist(err) {
		t.Fatalf("v2 residue not swept: %v", err)
	}
	if _, err := LoadManifest(vaultDir); err != nil {
		t.Fatalf("vault unusable after sweep: %v", err)
	}
}

// writeV2Vault lays down a minimal version-2 vault holding one project with one
// encrypted profile.
func writeV2Vault(t *testing.T, identity *age.X25519Identity, vaultDir string, plaintext []byte) {
	t.Helper()
	v2 := map[string]any{
		"version":    2,
		"recipients": []string{identity.Recipient().String()},
		"projects": map[string]any{
			"myapp": map[string]any{
				"profiles": map[string]any{
					"dev": map[string]any{
						"updated_at": time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
						"checksum":   Checksum(plaintext),
					},
				},
				"repositories": []string{"github.com/acme/myapp"},
			},
		},
	}
	data, err := json.MarshalIndent(v2, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(vaultDir, manifestName), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := Encrypt(plaintext, []age.Recipient{identity.Recipient()})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(vaultDir, "projects", "myapp", "dev.env.age"), ciphertext, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifestReadsConcurrentlyAddedProjects(t *testing.T) {
	identity, vaultDir := newStoreVault(t)
	recipients := []age.Recipient{identity.Recipient()}
	manifest := Manifest{
		Version:    ManifestVersion,
		Recipients: []string{identity.Recipient().String()},
		Projects:   map[string]Project{},
	}
	if _, err := manifest.EnsureProjectID("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(vaultDir, manifest); err != nil {
		t.Fatal(err)
	}

	// Another device adds beta on a disjoint path: an independently written
	// meta.age under its own id, exactly what a --ff-only merge would bring in.
	writeProjectMeta(t, vaultDir, recipients, Project{Name: "beta", Profiles: map[string]Profile{}})

	loaded, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Projects["alpha"]; !ok {
		t.Fatal("alpha missing after concurrent add")
	}
	if _, ok := loaded.Projects["beta"]; !ok {
		t.Fatal("beta (concurrently added) missing")
	}
}

func newStoreVault(t *testing.T) (*age.X25519Identity, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GITENV_CONFIG_DIR", filepath.Join(root, "config"))
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	SetSessionIdentity(identity)
	t.Cleanup(ClearSessionIdentity)
	return identity, filepath.Join(root, "vault")
}

func writeManifestJSON(t *testing.T, vaultDir string, m Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(vaultDir, manifestName), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeProjectMeta(t *testing.T, vaultDir string, recipients []age.Recipient, entry Project) {
	t.Helper()
	if entry.ID == "" {
		id, err := newID()
		if err != nil {
			t.Fatal(err)
		}
		entry.ID = id
	}
	payload, err := marshalProject(entry)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := Encrypt(payload, recipients)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(vaultDir, filepath.FromSlash(metadataRelPath(entry.ID)))
	if err := WriteAtomic(dest, ciphertext, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readMetaBytes(t *testing.T, vaultDir string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	entries, err := os.ReadDir(filepath.Join(vaultDir, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(vaultDir, "projects", e.Name(), "meta.age"))
		if err != nil {
			continue
		}
		result[e.Name()] = data
	}
	return result
}
