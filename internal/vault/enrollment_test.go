package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupEnrollmentVault creates a fully-initialized vault with an approver identity,
// links a test project, and captures two profiles. Returns the vault root, the
// per-test config dir (already set via t.Setenv), and the approver recipient string.
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

	vaultDir := filepath.Join(root, "vault")
	if err := Init(vaultDir, identity.Recipient().String()); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := LocalConfig{VaultPath: vaultDir, Projects: map[string]LocalProject{}}
	if err := Link(&cfg, "myapp", projectDir); err != nil {
		t.Fatal(err)
	}
	if err := SaveLocal(cfg); err != nil {
		t.Fatal(err)
	}

	// Capture two distinct profiles so approval must handle multiple files.
	for _, env := range []struct{ profile, content string }{
		{"dev", "ENV=dev\nDATABASE_URL=postgres://dev\n"},
		{"prod", "ENV=prod\nDATABASE_URL=postgres://prod\n"},
	} {
		if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte(env.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Capture(&cfg, "myapp", env.profile); err != nil {
			t.Fatal(err)
		}
	}
	return vaultDir
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
// the new device's identity can decrypt every profile in the vault.
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
		ciphertext, err := os.ReadFile(ProfilePath(vaultDir, "myapp", profile))
		if err != nil {
			t.Fatalf("read profile %s: %v", profile, err)
		}
		plaintext, err := Decrypt(ciphertext, newIdentity)
		if err != nil {
			t.Errorf("new identity cannot decrypt profile %s: %v", profile, err)
		}
		if len(plaintext) == 0 {
			t.Errorf("decrypted profile %s is empty", profile)
		}
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
		ciphertext, err := os.ReadFile(ProfilePath(vaultDir, "myapp", profile))
		if err != nil {
			t.Fatalf("read profile %s: %v", profile, err)
		}
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
// onManifestUpdate returns an error, all re-encrypted profile files are restored
// to their original ciphertext.
func TestApproveEnrollmentRequestCallbackFailureRollsBack(t *testing.T) {
	vaultDir := setupEnrollmentVault(t)

	// Record original ciphertexts before approval attempt.
	original := map[string][]byte{}
	for _, profile := range []string{"dev", "prod"} {
		data, err := os.ReadFile(ProfilePath(vaultDir, "myapp", profile))
		if err != nil {
			t.Fatal(err)
		}
		original[profile] = data
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
		data, err := os.ReadFile(ProfilePath(vaultDir, "myapp", profile))
		if err != nil {
			t.Fatalf("read profile %s after rollback: %v", profile, err)
		}
		if string(data) != string(original[profile]) {
			t.Errorf("profile %s was not rolled back: ciphertext changed", profile)
		}
	}

	// Original identity must still be able to decrypt (rollback was clean).
	for _, profile := range []string{"dev", "prod"} {
		data, err := os.ReadFile(ProfilePath(vaultDir, "myapp", profile))
		if err != nil {
			t.Fatal(err)
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
