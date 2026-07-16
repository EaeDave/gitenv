package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"filippo.io/age"
)

var (
	sessionIdentityMu sync.RWMutex
	sessionIdentity   *age.X25519Identity
)

func SetSessionIdentity(identity *age.X25519Identity) {
	sessionIdentityMu.Lock()
	sessionIdentity = identity
	sessionIdentityMu.Unlock()
}

func ClearSessionIdentity() {
	SetSessionIdentity(nil)
}

func GenerateIdentity() (*age.X25519Identity, error) {
	return age.GenerateX25519Identity()
}

func SaveIdentity(identity *age.X25519Identity) error {
	path, err := IdentityPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("identity already exists at %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return WriteAtomic(path, []byte(identity.String()+"\n"), 0o600)
}

func LoadIdentity() (*age.X25519Identity, error) {
	sessionIdentityMu.RLock()
	identity := sessionIdentity
	sessionIdentityMu.RUnlock()
	if identity != nil {
		return identity, nil
	}
	if identity, err := LoadIdentityFromKeychain(); err == nil {
		return identity, nil
	}
	return loadIdentityFallback()
}

func loadIdentityFallback() (*age.X25519Identity, error) {
	path, err := IdentityPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity %s: %w", path, err)
	}
	parsed, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	return parsed, nil
}

// LoadIdentityForManifest skips stale credential sources and caches the first
// identity authorized by the manifest for subsequent vault operations.
func LoadIdentityForManifest(manifest Manifest) (*age.X25519Identity, error) {
	sessionIdentityMu.RLock()
	session := sessionIdentity
	sessionIdentityMu.RUnlock()
	candidates := make([]*age.X25519Identity, 0, 3)
	if session != nil {
		candidates = append(candidates, session)
	}
	if identity, err := LoadIdentityFromKeychain(); err == nil && identity != session {
		candidates = append(candidates, identity)
	}
	if identity, err := loadIdentityFallback(); err == nil {
		candidates = append(candidates, identity)
	}
	for _, identity := range candidates {
		if IdentityUnlocks(manifest, identity) {
			SetSessionIdentity(identity)
			return identity, nil
		}
	}
	return nil, errors.New("no stored identity is authorized for this vault")
}

func ParseIdentity(data []byte) (*age.X25519Identity, error) {
	identity, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	return identity, nil
}

func ParseRecipients(values []string) ([]age.Recipient, error) {
	recipients := make([]age.Recipient, 0, len(values))
	for _, value := range values {
		recipient, err := age.ParseX25519Recipient(value)
		if err != nil {
			return nil, fmt.Errorf("parse recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}
	if len(recipients) == 0 {
		return nil, errors.New("vault has no recipients")
	}
	return recipients, nil
}

func Encrypt(plaintext []byte, recipients []age.Recipient) ([]byte, error) {
	var output bytes.Buffer
	writer, err := age.Encrypt(&output, recipients...)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(plaintext); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func Decrypt(ciphertext []byte, identity age.Identity) ([]byte, error) {
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}
