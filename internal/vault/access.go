package vault

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"filippo.io/age"
)

type IdentityStoreMode string

const (
	StoreIdentityKeychain IdentityStoreMode = "keychain"
	StoreIdentitySession  IdentityStoreMode = "session"
	StoreIdentityFallback IdentityStoreMode = "fallback"
)

func ValidateMasterPassword(password string) error {
	if utf8.RuneCountInString(password) < 12 {
		return errors.New("master password must contain at least 12 characters")
	}
	return nil
}

func ProtectVault(root, password, deviceName string, mode IdentityStoreMode) error {
	if err := ValidateMasterPassword(password); err != nil {
		return err
	}
	identity, err := LoadIdentity()
	if err != nil {
		return err
	}
	manifest, err := LoadManifest(root)
	if err != nil {
		return err
	}
	if manifest.WrappedIdentity != nil {
		return errors.New("vault already has master-password protection")
	}
	wrapped, err := WrapIdentity(identity, password, DefaultWrapParams)
	if err != nil {
		return err
	}
	manifest.Version = ManifestVersion
	manifest.WrappedIdentity = &wrapped
	if deviceName != "" && !hasDeviceRecipient(manifest.Devices, identity.Recipient().String()) {
		manifest.Devices = append(manifest.Devices, Device{ID: Checksum([]byte(identity.Recipient().String()))[:16], Name: deviceName, Recipient: identity.Recipient().String()})
	}
	if err := SaveManifest(root, manifest); err != nil {
		return err
	}
	return StoreUnlockedIdentity(identity, mode)
}

func UnlockVault(root, password string, mode IdentityStoreMode) error {
	manifest, err := LoadManifest(root)
	if err != nil {
		return err
	}
	if manifest.WrappedIdentity == nil {
		return errors.New("vault does not have master-password protection; import recovery identity or migrate it on an authorized device")
	}
	identity, err := UnwrapIdentity(*manifest.WrappedIdentity, password)
	if err != nil {
		return err
	}
	if !recipientAllowed(manifest.Recipients, identity.Recipient().String()) {
		return errors.New("wrapped identity is not an authorized vault recipient")
	}
	return StoreUnlockedIdentity(identity, mode)
}

func StoreUnlockedIdentity(identity *age.X25519Identity, mode IdentityStoreMode) error {
	switch mode {
	case StoreIdentityKeychain:
		SetSessionIdentity(identity)
		if err := SaveIdentityToKeychain(identity); err != nil && !errors.Is(err, ErrKeychainUnavailable) {
			return err
		}
		return nil
	case StoreIdentitySession:
		SetSessionIdentity(identity)
		return nil
	case StoreIdentityFallback:
		if err := SaveIdentityFallback(identity); err != nil {
			return err
		}
		SetSessionIdentity(identity)
		return nil
	default:
		return fmt.Errorf("unknown identity store mode %q", mode)
	}
}

func recipientAllowed(recipients []string, candidate string) bool {
	for _, recipient := range recipients {
		if recipient == candidate {
			return true
		}
	}
	return false
}
func IdentityUnlocks(manifest Manifest, identity *age.X25519Identity) bool {
	return identity != nil && recipientAllowed(manifest.Recipients, identity.Recipient().String())
}

func hasDeviceRecipient(devices []Device, recipient string) bool {
	for _, device := range devices {
		if device.Recipient == recipient {
			return true
		}
	}
	return false
}
