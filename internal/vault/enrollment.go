package vault

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
)

// Device represents a trusted device that has been enrolled in the vault.
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Recipient string `json:"recipient"`
}

// EnrollmentRequest is created by a new device seeking authorization.
// It contains only the device's public recipient key; the private key
// never leaves the requesting device.
type EnrollmentRequest struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Recipient string    `json:"recipient"`
	CreatedAt time.Time `json:"created_at"`
}

// enrollmentManifest is a superset of Manifest that carries enrollment fields.
// It is used internally so enrollment operations work before model.go gains
// the Devices/EnrollmentRequests fields; unknown JSON keys are preserved.
type enrollmentManifest struct {
	Version            int                 `json:"version"`
	Recipients         []string            `json:"recipients"`
	Projects           map[string]Project  `json:"projects"`
	Devices            []Device            `json:"devices"`
	EnrollmentRequests []EnrollmentRequest `json:"enrollment_requests"`
}

// profileBackup holds the original ciphertext of a profile before re-encryption,
// used for rollback if a later step fails.
type profileBackup struct {
	path string
	data []byte
}

// loadEnrollmentManifest reads the vault manifest and includes enrollment fields.
func loadEnrollmentManifest(root string) (enrollmentManifest, error) {
	data, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		return enrollmentManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m enrollmentManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return enrollmentManifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Projects == nil {
		m.Projects = map[string]Project{}
	}
	if m.Devices == nil {
		m.Devices = []Device{}
	}
	if m.EnrollmentRequests == nil {
		m.EnrollmentRequests = []EnrollmentRequest{}
	}
	return m, nil
}

// saveEnrollmentManifest writes the manifest, preserving any JSON keys written
// by other components (e.g., wrapped_identity added by another agent).
func saveEnrollmentManifest(root string, m enrollmentManifest) error {
	path := filepath.Join(root, manifestName)

	// Load existing raw bytes so we can preserve unknown keys.
	existing, readErr := os.ReadFile(path)

	// Marshal our struct.
	ourBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}

	// Merge: start from the existing raw map, overlay our fields.
	merged := make(map[string]json.RawMessage)
	if readErr == nil {
		_ = json.Unmarshal(existing, &merged) // ignore parse error; start fresh if corrupt
	}
	var ourMap map[string]json.RawMessage
	if err := json.Unmarshal(ourBytes, &ourMap); err != nil {
		return err
	}
	for k, v := range ourMap {
		merged[k] = v
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteAtomic(path, data, 0o600)
}

// rollbackProfiles restores profile files from their pre-approval backups.
// Errors during rollback are silently discarded to surface the original failure.
func rollbackProfiles(backups []profileBackup) {
	for _, b := range backups {
		_ = WriteAtomic(b.path, b.data, 0o600)
	}
}

// CreateEnrollmentRequest generates a fresh X25519 identity for this device and
// returns an EnrollmentRequest containing only the public recipient string. The
// caller must persist the returned identity locally (e.g., via SaveIdentity) and
// append the request to the shared vault manifest.
//
// The private key never enters the EnrollmentRequest.
func CreateEnrollmentRequest(name string) (*age.X25519Identity, EnrollmentRequest, error) {
	identity, err := GenerateIdentity()
	if err != nil {
		return nil, EnrollmentRequest{}, fmt.Errorf("generate identity: %w", err)
	}

	idBuf := make([]byte, 16)
	if _, err := rand.Read(idBuf); err != nil {
		return nil, EnrollmentRequest{}, fmt.Errorf("generate request id: %w", err)
	}

	req := EnrollmentRequest{
		ID:        hex.EncodeToString(idBuf),
		Name:      name,
		Recipient: identity.Recipient().String(),
		CreatedAt: time.Now().UTC(),
	}
	return identity, req, nil
}

// ApproveEnrollmentRequest authorizes a pending enrollment by re-encrypting every
// profile in the vault for the combined set of recipients (existing + new), then
// updating the manifest to record the new device and remove the request.
//
// approverIdentity must be able to decrypt all existing profile ciphertexts.
//
// onManifestUpdate is optional. When non-nil it is called with the serialized
// updated manifest (before disk write) so callers can stage it in git or run
// additional validation. If it returns an error, all re-encrypted profile files
// are rolled back to their original ciphertext.
//
// If any profile re-encryption or the manifest disk write fails, all profile
// files that were already overwritten are rolled back atomically.
func ApproveEnrollmentRequest(
	root string,
	approverIdentity age.Identity,
	requestID string,
	onManifestUpdate func([]byte) error,
) error {
	m, err := loadEnrollmentManifest(root)
	if err != nil {
		return err
	}

	// Locate the request.
	reqIdx := -1
	var req EnrollmentRequest
	for i, r := range m.EnrollmentRequests {
		if r.ID == requestID {
			reqIdx = i
			req = r
			break
		}
	}
	if reqIdx == -1 {
		return fmt.Errorf("enrollment request %q not found", requestID)
	}

	// Parse the new recipient's public key.
	newRecipient, err := age.ParseX25519Recipient(req.Recipient)
	if err != nil {
		return fmt.Errorf("parse enrollment recipient: %w", err)
	}

	// Build the full recipient list: existing + new.
	existing, err := ParseRecipients(m.Recipients)
	if err != nil {
		return err
	}
	allRecipients := make([]age.Recipient, len(existing)+1)
	copy(allRecipients, existing)
	allRecipients[len(existing)] = newRecipient

	// Collect every profile file path.
	type profileRef struct {
		path    string
		project string
		name    string
	}
	var profiles []profileRef
	for projName, proj := range m.Projects {
		for profName := range proj.Profiles {
			profiles = append(profiles, profileRef{
				path:    ProfilePath(root, projName, profName),
				project: projName,
				name:    profName,
			})
		}
	}

	// Re-encrypt each profile; accumulate backups so we can roll back on failure.
	var backups []profileBackup
	for _, p := range profiles {
		oldCiphertext, err := os.ReadFile(p.path)
		if err != nil {
			rollbackProfiles(backups)
			return fmt.Errorf("read profile %s/%s: %w", p.project, p.name, err)
		}

		plaintext, err := Decrypt(oldCiphertext, approverIdentity)
		if err != nil {
			rollbackProfiles(backups)
			return fmt.Errorf("decrypt profile %s/%s: %w", p.project, p.name, err)
		}

		newCiphertext, err := Encrypt(plaintext, allRecipients)
		if err != nil {
			rollbackProfiles(backups)
			return fmt.Errorf("encrypt profile %s/%s: %w", p.project, p.name, err)
		}

		if err := WriteAtomic(p.path, newCiphertext, 0o600); err != nil {
			// WriteAtomic uses temp+rename; original file is intact on failure.
			rollbackProfiles(backups)
			return fmt.Errorf("write profile %s/%s: %w", p.project, p.name, err)
		}
		// Record backup only after successful write so rollback knows what changed.
		backups = append(backups, profileBackup{path: p.path, data: oldCiphertext})
	}

	// Update manifest state.
	m.Recipients = append(m.Recipients, req.Recipient)
	m.Devices = append(m.Devices, Device{
		ID:        req.ID,
		Name:      req.Name,
		Recipient: req.Recipient,
	})
	// Remove the approved request (order not significant).
	last := len(m.EnrollmentRequests) - 1
	m.EnrollmentRequests[reqIdx] = m.EnrollmentRequests[last]
	m.EnrollmentRequests = m.EnrollmentRequests[:last]

	// Invoke the caller's manifest hook before writing to disk.
	if onManifestUpdate != nil {
		preview, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			rollbackProfiles(backups)
			return err
		}
		preview = append(preview, '\n')
		if err := onManifestUpdate(preview); err != nil {
			rollbackProfiles(backups)
			return fmt.Errorf("manifest update callback: %w", err)
		}
	}

	if err := saveEnrollmentManifest(root, m); err != nil {
		rollbackProfiles(backups)
		return fmt.Errorf("save manifest: %w", err)
	}

	return nil
}

// enrollmentRequestContainsNoPrivateKey returns true when r carries only public
// information. This exists as an internal invariant check used in tests.
func enrollmentRequestContainsNoPrivateKey(r EnrollmentRequest) bool {
	// An X25519 private key string begins with "AGE-SECRET-KEY-".
	// The Recipient field should begin with "age1".
	if len(r.Recipient) == 0 {
		return false
	}
	// Reject if the recipient value looks like a private key.
	const privatePrefix = "AGE-SECRET-KEY-"
	return len(r.Recipient) < len(privatePrefix) || r.Recipient[:len(privatePrefix)] != privatePrefix
}

// AddEnrollmentRequest appends a pending EnrollmentRequest to the vault manifest
// and saves it. Called by the requesting device to publish its request.
func AddEnrollmentRequest(root string, req EnrollmentRequest) error {
	m, err := loadEnrollmentManifest(root)
	if err != nil {
		return err
	}
	// Reject if a request with the same ID already exists.
	for _, existing := range m.EnrollmentRequests {
		if existing.ID == req.ID {
			return errors.New("enrollment request with this ID already exists")
		}
	}
	m.EnrollmentRequests = append(m.EnrollmentRequests, req)
	return saveEnrollmentManifest(root, m)
}
