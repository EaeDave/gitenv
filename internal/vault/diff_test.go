package vault

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

func TestCompareVaultSnapshotsReturnsValueFreeProfileDiff(t *testing.T) {
	identity := setupDiffIdentity(t)
	base := snapshotWithProfiles(t, identity, map[string][]byte{
		"api/dev": []byte("API_KEY=old-secret\n# DEBUG=true\nREMOVED=retired-secret\n"),
	})
	current := snapshotWithProfiles(t, identity, map[string][]byte{
		"api/dev": []byte("API_KEY=new-secret\nDEBUG=true\nADDED=private-value\n"),
	})
	var baseManifest, currentManifest Manifest
	_ = json.Unmarshal(base[manifestName], &baseManifest)
	_ = json.Unmarshal(current[manifestName], &currentManifest)
	if baseManifest.Projects["api"].Profiles["dev"].Checksum == currentManifest.Projects["api"].Profiles["dev"].Checksum {
		t.Fatal("snapshot fixtures unexpectedly have the same checksum")
	}
	delta, err := CompareVaultSnapshots(base, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Profiles) != 1 || delta.Profiles[0].Project != "api" || delta.Profiles[0].Profile != "dev" {
		t.Fatalf("unexpected profile delta: %#v", delta)
	}
	changes := delta.Profiles[0].Diff.Changes
	if len(changes) != 4 {
		t.Fatalf("changes = %#v", changes)
	}
	dump := fmt.Sprintf("%#v", delta)
	for _, secret := range []string{"old-secret", "new-secret", "retired-secret", "private-value"} {
		if strings.Contains(dump, secret) {
			t.Fatalf("vault delta exposed %q: %s", secret, dump)
		}
	}
}

func TestCompareVaultSnapshotsShowsProfileLifecycleAndCiphertextRefresh(t *testing.T) {
	identity := setupDiffIdentity(t)
	base := snapshotWithProfiles(t, identity, map[string][]byte{
		"api/dev":    []byte("A=same\n"),
		"api/legacy": []byte("OLD=value\n"),
	})
	current := snapshotWithProfiles(t, identity, map[string][]byte{
		"api/dev":  []byte("A=same\n"),
		"api/prod": []byte("NEW=value\n"),
	})
	delta, err := CompareVaultSnapshots(base, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Profiles) != 3 {
		t.Fatalf("profile deltas = %#v", delta.Profiles)
	}
	kinds := map[string]ProfileDeltaKind{}
	for _, profile := range delta.Profiles {
		kinds[profile.Profile] = profile.Kind
	}
	if kinds["dev"] != ProfileChanged || kinds["legacy"] != ProfileRemoved || kinds["prod"] != ProfileAdded {
		t.Fatalf("profile lifecycle = %#v", kinds)
	}
	if !delta.Profiles[0].Diff.Empty() {
		t.Fatalf("identical recapture reported content changes: %#v", delta.Profiles[0].Diff)
	}
}

func TestCompareVaultSnapshotsRejectsManifestChecksumMismatch(t *testing.T) {
	identity := setupDiffIdentity(t)
	base := snapshotWithProfiles(t, identity, map[string][]byte{"api/dev": []byte("A=old\n")})
	current := snapshotWithProfiles(t, identity, map[string][]byte{"api/dev": []byte("A=new\n")})
	var manifest Manifest
	if err := json.Unmarshal(current[manifestName], &manifest); err != nil {
		t.Fatal(err)
	}
	profile := manifest.Projects["api"].Profiles["dev"]
	profile.Checksum = Checksum([]byte("tampered"))
	project := manifest.Projects["api"]
	project.Profiles["dev"] = profile
	manifest.Projects["api"] = project
	current[manifestName], _ = json.Marshal(manifest)
	if _, err := CompareVaultSnapshots(base, current); err == nil {
		t.Fatal("snapshot with mismatched checksum was accepted")
	}
}

func setupDiffIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()
	t.Setenv("GITENV_CONFIG_DIR", t.TempDir())
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	return identity
}

func snapshotWithProfiles(t *testing.T, identity *age.X25519Identity, profiles map[string][]byte) map[string][]byte {
	t.Helper()
	manifest := Manifest{Version: ManifestVersion, Recipients: []string{identity.Recipient().String()}, Projects: map[string]Project{}}
	files := map[string][]byte{".gitignore": []byte("*.plaintext\n")}
	stamp := time.Now().UTC()
	for ref, plaintext := range profiles {
		parts := strings.Split(ref, "/")
		if len(parts) != 2 {
			t.Fatalf("invalid fixture profile ref %q", ref)
		}
		project, profile := parts[0], parts[1]
		ciphertext, err := Encrypt(plaintext, []age.Recipient{identity.Recipient()})
		if err != nil {
			t.Fatal(err)
		}
		entry := manifest.Projects[project]
		if entry.Profiles == nil {
			entry.Profiles = map[string]Profile{}
		}
		entry.Profiles[profile] = Profile{Checksum: Checksum(plaintext), UpdatedAt: stamp}
		manifest.Projects[project] = entry
		files[path.Join("projects", project, profile+".env.age")] = ciphertext
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files[manifestName] = manifestJSON
	var roundTrip Manifest
	if err := json.Unmarshal(files[manifestName], &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(profiles) > 0 && len(roundTrip.Projects) == 0 {
		t.Fatalf("snapshot fixture lost projects: %s", files[manifestName])
	}
	return files
}
