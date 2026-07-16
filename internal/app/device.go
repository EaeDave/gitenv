package app

import (
	"errors"
	"fmt"

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
		return fmt.Errorf("device enrollment %q is not approved yet", requestID)
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
