package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

// setupEnrollmentVault builds a v3 vault directly, avoiding service.go (owned by
// another slice): a saved approver identity, a project "myapp" with two encrypted
// profiles, and the encrypted metadata that ties them together. It returns the
// vault root; the per-test config dir is set via t.Setenv.
func setupEnrollmentVault(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	configDir := filepath.Join(root, "config")
	t.Setenv("GITENV_CONFIG_DIR", configDir)

	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(identity); err != nil {
		t.Fatal(err)
	}
	// Pin the session so LoadManifest is deterministic across tests.
	SetSessionIdentity(identity)
	t.Cleanup(ClearSessionIdentity)

	vaultDir := filepath.Join(root, "vault")
	recipients := []age.Recipient{identity.Recipient()}
	manifest := Manifest{
		Version:    ManifestVersion,
		Recipients: []string{identity.Recipient().String()},
		Projects:   map[string]Project{},
	}
	// Two distinct profiles so approval must handle multiple files.
	for _, env := range []struct{ profile, content string }{
		{"dev", "ENV=dev\nDATABASE_URL=postgres://dev\n"},
		{"prod", "ENV=prod\nDATABASE_URL=postgres://prod\n"},
	} {
		profileID, err := manifest.EnsureProfileID("myapp", env.profile)
		if err != nil {
			t.Fatal(err)
		}
		entry := manifest.Projects["myapp"]
		stored := entry.Profiles[env.profile]
		stored.Checksum = Checksum([]byte(env.content))
		stored.UpdatedAt = time.Now().UTC()
		entry.Profiles[env.profile] = stored
		manifest.Projects["myapp"] = entry

		ciphertext, err := Encrypt([]byte(env.content), recipients)
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(vaultDir, filepath.FromSlash(profileRelPath(entry.ID, profileID)))
		if err := WriteAtomic(dest, ciphertext, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveManifest(vaultDir, manifest); err != nil {
		t.Fatal(err)
	}
	return vaultDir
}

// profileCiphertext resolves and reads a profile ciphertext through the v3
// layout, loading the manifest to map names to random ids.
func profileCiphertext(t *testing.T, vaultDir, project, profile string) []byte {
	t.Helper()
	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	path, ok := ProfilePath(vaultDir, manifest, project, profile)
	if !ok {
		t.Fatalf("profile %s/%s has no ciphertext path", project, profile)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestCreateEnrollmentRequestNoPrivateKey verifies that the returned
// EnrollmentRequest contains only a public recipient key, never a private key.
func TestCreateEnrollmentRequestNoPrivateKey(t *testing.T) {
	_, req, err := CreateEnrollmentRequest("laptop")
	if err != nil {
		t.Fatal(err)
	}

	if req.ID == "" {
		t.Error("request ID must not be empty")
	}
	if req.Name != "laptop" {
		t.Errorf("name: got %q, want %q", req.Name, "laptop")
	}
	if req.Recipient == "" {
		t.Error("recipient must not be empty")
	}
	// age public keys start with "age1"; private keys start with "AGE-SECRET-KEY-"
	if strings.HasPrefix(req.Recipient, "AGE-SECRET-KEY-") {
		t.Error("EnrollmentRequest.Recipient contains a private key")
	}
	if !strings.HasPrefix(req.Recipient, "age1") {
		t.Errorf("Recipient %q does not look like an age public key", req.Recipient)
	}
	if !enrollmentRequestContainsNoPrivateKey(req) {
		t.Error("enrollmentRequestContainsNoPrivateKey returned false")
	}
}

// TestApproveEnrollmentRequestNewIdentityDecryptsAll verifies that after approval
// the new device's identity can decrypt every profile AND the project metadata.
func TestApproveEnrollmentRequestNewIdentityDecryptsAll(t *testing.T) {
	vaultDir := setupEnrollmentVault(t)

	newIdentity, req, err := CreateEnrollmentRequest("newdevice")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddEnrollmentRequest(vaultDir, req); err != nil {
		t.Fatal(err)
	}

	approver, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApproveEnrollmentRequest(vaultDir, approver, req.ID, nil); err != nil {
		t.Fatal(err)
	}

	// New identity must decrypt both profiles.
	for _, profile := range []string{"dev", "prod"} {
		ciphertext := profileCiphertext(t, vaultDir, "myapp", profile)
		plaintext, err := Decrypt(ciphertext, newIdentity)
		if err != nil {
			t.Errorf("new identity cannot decrypt profile %s: %v", profile, err)
		}
		if len(plaintext) == 0 {
			t.Errorf("decrypted profile %s is empty", profile)
		}
	}

	// The point of re-encrypting meta.age: the new device must read metadata too,
	// otherwise it clones the vault and sees zero projects.
	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	dir, ok := ProjectDir(vaultDir, manifest, "myapp")
	if !ok {
		t.Fatal("project directory unresolved")
	}
	metaCiphertext, err := os.ReadFile(filepath.Join(dir, metadataName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(metaCiphertext, newIdentity); err != nil {
		t.Errorf("new identity cannot decrypt project metadata: %v", err)
	}
}

// TestApproveEnrollmentRequestOldIdentityStillWorks verifies that the original
// device identity remains valid after a new device is enrolled.
func TestApproveEnrollmentRequestOldIdentityStillWorks(t *testing.T) {
	vaultDir := setupEnrollmentVault(t)

	_, req, err := CreateEnrollmentRequest("second-device")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddEnrollmentRequest(vaultDir, req); err != nil {
		t.Fatal(err)
	}

	approver, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApproveEnrollmentRequest(vaultDir, approver, req.ID, nil); err != nil {
		t.Fatal(err)
	}

	// Original identity must still decrypt both profiles.
	for _, profile := range []string{"dev", "prod"} {
		ciphertext := profileCiphertext(t, vaultDir, "myapp", profile)
		if _, err := Decrypt(ciphertext, approver); err != nil {
			t.Errorf("original identity cannot decrypt profile %s after enrollment: %v", profile, err)
		}
	}
}

// TestApproveEnrollmentRequestManifestState verifies that after approval the
// manifest: adds the new recipient, creates a Device entry, and removes the
// EnrollmentRequest.
func TestApproveEnrollmentRequestManifestState(t *testing.T) {
	vaultDir := setupEnrollmentVault(t)

	_, req, err := CreateEnrollmentRequest("tablet")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddEnrollmentRequest(vaultDir, req); err != nil {
		t.Fatal(err)
	}

	approver, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApproveEnrollmentRequest(vaultDir, approver, req.ID, nil); err != nil {
		t.Fatal(err)
	}

	m, err := loadEnrollmentManifest(vaultDir)
	if err != nil {
		t.Fatal(err)
	}

	// Request must be gone.
	for _, r := range m.EnrollmentRequests {
		if r.ID == req.ID {
			t.Error("approved request still present in EnrollmentRequests")
		}
	}

	// New recipient must be in Recipients.
	found := false
	for _, r := range m.Recipients {
		if r == req.Recipient {
			found = true
			break
		}
	}
	if !found {
		t.Error("new recipient not added to manifest Recipients")
	}

	// Device entry must be present.
	foundDevice := false
	for _, d := range m.Devices {
		if d.ID == req.ID && d.Name == req.Name && d.Recipient == req.Recipient {
			foundDevice = true
			break
		}
	}
	if !foundDevice {
		t.Error("Device entry not added to manifest after approval")
	}
}

// TestApproveEnrollmentRequestCallbackFailureRollsBack verifies that when
// onManifestUpdate returns an error, all re-encrypted files are restored to
// their original ciphertext.
func TestApproveEnrollmentRequestCallbackFailureRollsBack(t *testing.T) {
	vaultDir := setupEnrollmentVault(t)

	// Record original ciphertexts before the approval attempt.
	original := map[string][]byte{}
	for _, profile := range []string{"dev", "prod"} {
		original[profile] = profileCiphertext(t, vaultDir, "myapp", profile)
	}

	_, req, err := CreateEnrollmentRequest("failing-device")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddEnrollmentRequest(vaultDir, req); err != nil {
		t.Fatal(err)
	}

	approver, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}

	cbErr := errors.New("git staging failed")
	err = ApproveEnrollmentRequest(vaultDir, approver, req.ID, func([]byte) error {
		return cbErr
	})
	if err == nil {
		t.Fatal("expected error from failing callback, got nil")
	}
	if !errors.Is(err, cbErr) {
		t.Errorf("expected wrapped cbErr, got: %v", err)
	}

	// Profile files must be identical to pre-approval originals.
	for _, profile := range []string{"dev", "prod"} {
		data := profileCiphertext(t, vaultDir, "myapp", profile)
		if string(data) != string(original[profile]) {
			t.Errorf("profile %s was not rolled back: ciphertext changed", profile)
		}
		if _, err := Decrypt(data, approver); err != nil {
			t.Errorf("original identity cannot decrypt rolled-back profile %s: %v", profile, err)
		}
	}
}

// TestApproveEnrollmentRequestUnknownIDError verifies that approving a non-existent
// request returns an error without modifying any files.
func TestApproveEnrollmentRequestUnknownIDError(t *testing.T) {
	vaultDir := setupEnrollmentVault(t)

	approver, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	err = ApproveEnrollmentRequest(vaultDir, approver, "does-not-exist", nil)
	if err == nil {
		t.Fatal("expected error for unknown request ID")
	}
}

// TestAddEnrollmentRequestDuplicateIDRejected verifies that adding a request with
// the same ID twice returns an error.
func TestAddEnrollmentRequestDuplicateIDRejected(t *testing.T) {
	vaultDir := setupEnrollmentVault(t)

	_, req, err := CreateEnrollmentRequest("dup-device")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddEnrollmentRequest(vaultDir, req); err != nil {
		t.Fatal(err)
	}
	if err := AddEnrollmentRequest(vaultDir, req); err == nil {
		t.Error("expected error adding duplicate enrollment request ID")
	}
}
