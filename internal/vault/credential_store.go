package vault

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "gitenv"
	keyringAccount = "default-identity"
)

func keyringServiceName() string {
	override := os.Getenv("GITENV_CONFIG_DIR")
	if override == "" {
		return keyringService
	}
	sum := sha256.Sum256([]byte(filepath.Clean(override)))
	return fmt.Sprintf("%s-%x", keyringService, sum[:8])
}

var (
	ErrKeychainUnavailable = errors.New("OS keychain unavailable")
	ErrIdentityNotStored   = errors.New("identity not stored in OS keychain")
)

func SaveIdentityToKeychain(identity *age.X25519Identity) error {
	if identity == nil {
		return errors.New("identity is required")
	}
	if err := keyring.Set(keyringServiceName(), keyringAccount, identity.String()); err != nil {
		return fmt.Errorf("%w: %v", ErrKeychainUnavailable, err)
	}
	return nil
}

func LoadIdentityFromKeychain() (*age.X25519Identity, error) {
	value, err := keyring.Get(keyringServiceName(), keyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrIdentityNotStored
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeychainUnavailable, err)
	}
	return ParseIdentity([]byte(value))
}

func SavePendingIdentity(requestID string, identity *age.X25519Identity) error {
	if requestID == "" || identity == nil {
		return errors.New("request ID and identity are required")
	}
	if err := keyring.Set(keyringServiceName(), "enrollment-"+requestID, identity.String()); err == nil {
		return nil
	}
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(dir, "enrollment-"+requestID+".txt"), []byte(identity.String()+"\n"), 0o600)
}

func LoadPendingIdentity(requestID string) (*age.X25519Identity, error) {
	if value, err := keyring.Get(keyringServiceName(), "enrollment-"+requestID); err == nil {
		return ParseIdentity([]byte(value))
	}
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "enrollment-"+requestID+".txt"))
	if err != nil {
		return nil, fmt.Errorf("read pending enrollment identity: %w", err)
	}
	return ParseIdentity(data)
}
func DeletePendingIdentity(requestID string) error {
	keyringErr := keyring.Delete(keyringServiceName(), "enrollment-"+requestID)
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	fileErr := os.Remove(filepath.Join(dir, "enrollment-"+requestID+".txt"))
	if fileErr != nil && !errors.Is(fileErr, os.ErrNotExist) {
		return fileErr
	}
	if keyringErr != nil && !errors.Is(keyringErr, keyring.ErrNotFound) {
		return fmt.Errorf("remove pending keychain identity: %w", keyringErr)
	}
	return nil
}

func SaveIdentityFallback(identity *age.X25519Identity) error {
	path, err := IdentityPath()
	if err != nil {
		return err
	}
	return WriteAtomic(path, []byte(identity.String()+"\n"), 0o600)
}

func RemoveStoredIdentity() error {
	keyringErr := keyring.Delete(keyringServiceName(), keyringAccount)
	path, pathErr := IdentityPath()
	if pathErr == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			pathErr = err
		}
	}
	if pathErr != nil {
		return pathErr
	}
	if keyringErr != nil && !errors.Is(keyringErr, keyring.ErrNotFound) {
		return fmt.Errorf("remove keychain identity: %w", keyringErr)
	}
	return nil
}
