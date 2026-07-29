package vault

import (
	"encoding/json"
	"fmt"
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
	baseManifest, err := manifestFromSnapshot(base, identity)
	if err != nil {
		t.Fatal(err)
	}
	currentManifest, err := manifestFromSnapshot(current, identity)
	if err != nil {
		t.Fatal(err)
	}
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

func TestCompareVaultSnapshotsRejectsProfileChecksumMismatch(t *testing.T) {
	identity := setupDiffIdentity(t)
	base := snapshotWithProfiles(t, identity, map[string][]byte{"api/dev": []byte("A=old\n")})
	current := snapshotWithProfiles(t, identity, map[string][]byte{"api/dev": []byte("A=new\n")})
	// Corrupt the checksum recorded inside the current project's encrypted
	// metadata so it no longer matches the ciphertext age can decrypt.
	tamperProfileChecksum(t, current, identity, "api", "dev", Checksum([]byte("tampered")))
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
	// Pin the session so CompareVaultSnapshots' internal LoadIdentity is
	// deterministic and cannot pick up a leaked identity from another test.
	SetSessionIdentity(identity)
	t.Cleanup(ClearSessionIdentity)
	return identity
}

// snapshotWithProfiles builds a v3 vault snapshot (slash-keyed file map) with the
// given profiles. Each project and profile is assigned a random id, its env bytes
// are encrypted under projects/<projectID>/<profileID>.age, and its metadata is
// encrypted under projects/<projectID>/meta.age. gitenv.json carries only the
// plaintext bootstrap fields.
func snapshotWithProfiles(t *testing.T, identity *age.X25519Identity, profiles map[string][]byte) map[string][]byte {
	t.Helper()
	recipients := []age.Recipient{identity.Recipient()}
	files := map[string][]byte{".gitignore": []byte("*.plaintext\n")}
	stamp := time.Now().UTC()

	byProject := map[string]map[string][]byte{}
	for ref, plaintext := range profiles {
		parts := strings.Split(ref, "/")
		if len(parts) != 2 {
			t.Fatalf("invalid fixture profile ref %q", ref)
		}
		project, profile := parts[0], parts[1]
		if byProject[project] == nil {
			byProject[project] = map[string][]byte{}
		}
		byProject[project][profile] = plaintext
	}

	for project, profs := range byProject {
		projectID, err := newID()
		if err != nil {
			t.Fatal(err)
		}
		entry := Project{ID: projectID, Name: project, Profiles: map[string]Profile{}}
		for profile, plaintext := range profs {
			profileID, err := newID()
			if err != nil {
				t.Fatal(err)
			}
			ciphertext, err := Encrypt(plaintext, recipients)
			if err != nil {
				t.Fatal(err)
			}
			files[profileRelPath(projectID, profileID)] = ciphertext
			entry.Profiles[profile] = Profile{ID: profileID, Checksum: Checksum(plaintext), UpdatedAt: stamp}
		}
		payload, err := marshalProject(entry)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := Encrypt(payload, recipients)
		if err != nil {
			t.Fatal(err)
		}
		files[metadataRelPath(projectID)] = meta
	}

	manifest := Manifest{Version: ManifestVersion, Recipients: []string{identity.Recipient().String()}}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files[manifestName] = manifestJSON
	return files
}

// tamperProfileChecksum rewrites the stored checksum of one profile inside a
// snapshot's encrypted metadata, re-encrypting the meta.age in place.
func tamperProfileChecksum(t *testing.T, files map[string][]byte, identity *age.X25519Identity, project, profile, checksum string) {
	t.Helper()
	for key, ciphertext := range files {
		id, ok := metadataSnapshotID(key)
		if !ok {
			continue
		}
		plaintext, err := Decrypt(ciphertext, identity)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := unmarshalProject(plaintext, id)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name != project {
			continue
		}
		stored := entry.Profiles[profile]
		stored.Checksum = checksum
		entry.Profiles[profile] = stored
		payload, err := marshalProject(entry)
		if err != nil {
			t.Fatal(err)
		}
		reencrypted, err := Encrypt(payload, []age.Recipient{identity.Recipient()})
		if err != nil {
			t.Fatal(err)
		}
		files[key] = reencrypted
		return
	}
	t.Fatalf("meta.age for project %q not found in snapshot", project)
}
