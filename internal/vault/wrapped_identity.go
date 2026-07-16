package vault

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"filippo.io/age"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	wrappedIdentityAlgorithm = "argon2id+xchacha20poly1305"
	wrapSaltSize             = 16 // bytes
	wrapKeySize              = 32 // bytes — 256-bit key for XChaCha20-Poly1305
	maxWrapTime              = 5
	maxWrapMemory            = 256 * 1024 // 256 MiB hard ceiling for untrusted manifests
	maxWrapThreads           = 16
	maxWrappedCiphertext     = 1024
)

// WrappedIdentityParams holds Argon2id cost tuning parameters.
// Both WrapIdentity callers and the persisted WrappedIdentity embed these.
type WrappedIdentityParams struct {
	Time    uint32
	Memory  uint32
	Threads uint8
}

// DefaultWrapParams are secure interactive-CLI parameters (≈80–150 ms on
// commodity hardware). Adjust upward as hardware improves.
var DefaultWrapParams = WrappedIdentityParams{
	Time:    2,
	Memory:  64 * 1024, // 64 MiB
	Threads: 4,
}

// FastWrapParams are deliberately weak parameters for use in tests only.
// Never use in production.
var FastWrapParams = WrappedIdentityParams{
	Time:    1,
	Memory:  64, // 64 KiB — well above argon2's 8*threads minimum
	Threads: 1,
}

// WrappedIdentity is a self-contained, JSON-serializable container for a
// password-wrapped age X25519 private key. Every field needed to reproduce
// decryption is present; the password is never stored.
type WrappedIdentity struct {
	Algorithm  string `json:"algorithm"`
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	Time       uint32 `json:"time"`
	Memory     uint32 `json:"memory"`
	Threads    uint8  `json:"threads"`
}

// WrapIdentity encrypts the age X25519 private key with password using
// Argon2id key derivation and XChaCha20-Poly1305 authenticated encryption.
// The returned WrappedIdentity is fully self-contained and safe to serialize.
func WrapIdentity(identity *age.X25519Identity, password string, params WrappedIdentityParams) (WrappedIdentity, error) {
	if identity == nil {
		return WrappedIdentity{}, errors.New("wrap identity: identity is required")
	}
	if err := validateWrapParams(params); err != nil {
		return WrappedIdentity{}, fmt.Errorf("wrap identity: %w", err)
	}
	salt := make([]byte, wrapSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return WrappedIdentity{}, fmt.Errorf("wrap identity: generate salt: %w", err)
	}

	key := wrapDeriveKey([]byte(password), salt, params)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return WrappedIdentity{}, fmt.Errorf("wrap identity: init cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize()) // 24 bytes for XChaCha20
	if _, err := rand.Read(nonce); err != nil {
		return WrappedIdentity{}, fmt.Errorf("wrap identity: generate nonce: %w", err)
	}

	// Seal appends the 16-byte Poly1305 tag; wrong password → Open fails.
	ciphertext := aead.Seal(nil, nonce, []byte(identity.String()), nil)

	return WrappedIdentity{
		Algorithm:  wrappedIdentityAlgorithm,
		Salt:       salt,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Time:       params.Time,
		Memory:     params.Memory,
		Threads:    params.Threads,
	}, nil
}

// UnwrapIdentity decrypts w using password and returns the age X25519 identity.
// Returns a non-nil error if the password is wrong or the ciphertext has been
// tampered with; the error message is intentionally generic to avoid leaking
// distinguishing information.
func UnwrapIdentity(w WrappedIdentity, password string) (*age.X25519Identity, error) {
	if w.Algorithm != wrappedIdentityAlgorithm {
		return nil, fmt.Errorf("unwrap identity: unsupported algorithm %q", w.Algorithm)
	}
	if len(w.Salt) != wrapSaltSize || len(w.Nonce) != chacha20poly1305.NonceSizeX || len(w.Ciphertext) == 0 || len(w.Ciphertext) > maxWrappedCiphertext {
		return nil, errors.New("unwrap identity: invalid wrapped identity dimensions")
	}
	params := WrappedIdentityParams{Time: w.Time, Memory: w.Memory, Threads: w.Threads}
	if err := validateWrapParams(params); err != nil {
		return nil, fmt.Errorf("unwrap identity: %w", err)
	}

	key := wrapDeriveKey([]byte(password), w.Salt, params)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("unwrap identity: init cipher: %w", err)
	}

	plaintext, err := aead.Open(nil, w.Nonce, w.Ciphertext, nil)
	if err != nil {
		// Do NOT expose the underlying error — it distinguishes cipher failure
		// from message-authentication failure which leaks timing information.
		return nil, errors.New("unwrap identity: wrong password or tampered ciphertext")
	}

	id, err := age.ParseX25519Identity(strings.TrimSpace(string(plaintext)))
	if err != nil {
		return nil, fmt.Errorf("unwrap identity: parse age identity: %w", err)
	}
	return id, nil
}

func validateWrapParams(p WrappedIdentityParams) error {
	if p.Time == 0 || p.Time > maxWrapTime {
		return errors.New("invalid Argon2 time cost")
	}
	if p.Threads == 0 || p.Threads > maxWrapThreads {
		return errors.New("invalid Argon2 thread count")
	}
	if p.Memory < 8*uint32(p.Threads) || p.Memory > maxWrapMemory {
		return errors.New("invalid Argon2 memory cost")
	}
	return nil
}

// wrapDeriveKey runs Argon2id and returns a 32-byte key.
func wrapDeriveKey(password, salt []byte, p WrappedIdentityParams) []byte {
	return argon2.IDKey(password, salt, p.Time, p.Memory, p.Threads, wrapKeySize)
}
