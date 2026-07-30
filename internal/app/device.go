package app

import (
	"errors"
	"fmt"
	"sort"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

func RequestDeviceEnrollment(cfg *vault.LocalConfig, deviceName string) (vault.EnrollmentRequest, error) {
	if cfg.VaultPath == "" {
		return vault.EnrollmentRequest{}, errors.New("no vault configured")
	}
	if !HasRemote(*cfg) {
		return vault.EnrollmentRequest{}, errors.New("device enrollment requires a configured vault sync repository")
	}
	if cfg.PendingEnrollmentID != "" {
		return vault.EnrollmentRequest{}, fmt.Errorf("enrollment %q is already pending", cfg.PendingEnrollmentID)
	}
	identity, request, err := vault.CreateEnrollmentRequest(deviceName)
	if err != nil {
		return vault.EnrollmentRequest{}, err
	}
	if err := vault.SavePendingIdentity(request.ID, identity); err != nil {
		return vault.EnrollmentRequest{}, err
	}
	if err := vault.AddEnrollmentRequest(cfg.VaultPath, request); err != nil {
		return vault.EnrollmentRequest{}, err
	}
	cfg.PendingEnrollmentID = request.ID
	if err := vault.SaveLocal(*cfg); err != nil {
		return vault.EnrollmentRequest{}, err
	}
	if err := gitops.CommitAndPush(cfg.VaultPath, "gitenv: request device enrollment"); err != nil {
		return vault.EnrollmentRequest{}, err
	}
	return request, nil
}

func ApproveDeviceEnrollment(cfg vault.LocalConfig, requestID string) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	if !HasRemote(cfg) {
		return errors.New("device enrollment requires a configured vault sync repository")
	}
	if err := gitops.Pull(cfg.VaultPath); err != nil {
		return err
	}
	identity, err := vault.LoadIdentity()
	if err != nil {
		return err
	}
	if err := vault.ApproveEnrollmentRequest(cfg.VaultPath, identity, requestID, nil); err != nil {
		return err
	}
	return gitops.CommitAndPush(cfg.VaultPath, "gitenv: approve device enrollment")
}

// RejectDeviceEnrollment pulls the current queue, removes one pending request,
// then publishes that decision. Unlike approval, rejection never loads an
// identity or re-encrypts vault content because it grants no access.
func RejectDeviceEnrollment(cfg vault.LocalConfig, requestID string) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	if !HasRemote(cfg) {
		return errors.New("device enrollment requires a configured vault sync repository")
	}
	if err := gitops.Pull(cfg.VaultPath); err != nil {
		return err
	}
	if err := vault.RejectEnrollmentRequest(cfg.VaultPath, requestID); err != nil {
		return err
	}
	return gitops.CommitAndPush(cfg.VaultPath, "gitenv: reject device enrollment")
}

func ActivateDeviceEnrollment(cfg *vault.LocalConfig, requestID string, mode vault.IdentityStoreMode) error {
	if cfg.VaultPath == "" {
		return errors.New("no vault configured")
	}
	if !HasRemote(*cfg) {
		return errors.New("device enrollment requires a configured vault sync repository")
	}
	if err := gitops.Pull(cfg.VaultPath); err != nil {
		return err
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	identity, err := vault.LoadPendingIdentity(requestID)
	if err != nil {
		return err
	}
	if !manifestHasRecipient(manifest, identity.Recipient().String()) {
		if manifestHasEnrollmentRequest(manifest, requestID) {
			return fmt.Errorf("device enrollment %q is not approved yet", requestID)
		}
		// The shared request disappeared without this recipient being added, so a
		// trusted device rejected it. Clear local pending state to allow retry.
		_ = vault.DeletePendingIdentity(requestID)
		cfg.PendingEnrollmentID = ""
		if err := vault.SaveLocal(*cfg); err != nil {
			return err
		}
		return fmt.Errorf("device enrollment %q was rejected; request approval again if needed", requestID)
	}
	if err := vault.StoreUnlockedIdentity(identity, mode); err != nil {
		return err
	}
	_ = vault.DeletePendingIdentity(requestID)
	cfg.PendingEnrollmentID = ""
	return vault.SaveLocal(*cfg)
}

func manifestHasRecipient(manifest vault.Manifest, recipient string) bool {
	for _, candidate := range manifest.Recipients {
		if candidate == recipient {
			return true
		}
	}
	return false
}

func manifestHasEnrollmentRequest(manifest vault.Manifest, requestID string) bool {
	for _, request := range manifest.EnrollmentRequests {
		if request.ID == requestID {
			return true
		}
	}
	return false
}

// PendingApprovals returns the enrollment requests waiting in the vault,
// excluding the one this computer created, sorted oldest first. A computer is
// never shown its own request as something to approve: doing so would let a
// user believe they had authorized themselves, when approval can only come
// from a device the vault already trusts.
func PendingApprovals(cfg vault.LocalConfig, manifest vault.Manifest) []vault.EnrollmentRequest {
	pending := make([]vault.EnrollmentRequest, 0, len(manifest.EnrollmentRequests))
	for _, request := range manifest.EnrollmentRequests {
		if request.ID == cfg.PendingEnrollmentID {
			continue
		}
		pending = append(pending, request)
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	return pending
}

// EnrolledDevices returns the devices the vault already trusts, sorted by name
// so the roster reads the same on every computer.
func EnrolledDevices(manifest vault.Manifest) []vault.Device {
	devices := make([]vault.Device, len(manifest.Devices))
	copy(devices, manifest.Devices)
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Name < devices[j].Name
	})
	return devices
}
