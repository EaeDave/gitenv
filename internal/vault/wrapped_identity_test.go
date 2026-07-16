package vault

import (
	"encoding/json"
	"testing"
)

// All tests use FastWrapParams to keep the suite fast while still exercising
// the full Argon2id + XChaCha20-Poly1305 code path.

func TestWrapUnwrapRoundTrip(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	const password = "correct-horse-battery-staple"
	w, err := WrapIdentity(identity, password, FastWrapParams)
	if err != nil {
		t.Fatal(err)
	}

	// WrappedIdentity must survive a JSON marshal/unmarshal round-trip before
	// being usable (it will be persisted in the manifest).
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var w2 WrappedIdentity
	if err := json.Unmarshal(data, &w2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, err := UnwrapIdentity(w2, password)
	if err != nil {
		t.Fatalf("UnwrapIdentity: %v", err)
	}
	if got.String() != identity.String() {
		t.Fatalf("private key mismatch:\n got  %q\n want %q", got.String(), identity.String())
	}
	if got.Recipient().String() != identity.Recipient().String() {
		t.Fatalf("recipient mismatch:\n got  %q\n want %q", got.Recipient(), identity.Recipient())
	}
}

func TestUnwrapWrongPassword(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	w, err := WrapIdentity(identity, "correct", FastWrapParams)
	if err != nil {
		t.Fatal(err)
	}

	_, err = UnwrapIdentity(w, "wrong")
	if err == nil {
		t.Fatal("UnwrapIdentity with wrong password: expected error, got nil")
	}
}

func TestUnwrapTamperedCiphertext(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	const password = "password"
	w, err := WrapIdentity(identity, password, FastWrapParams)
	if err != nil {
		t.Fatal(err)
	}

	// Flip the first byte of the ciphertext (which includes the Poly1305 tag).
	tampered := make([]byte, len(w.Ciphertext))
	copy(tampered, w.Ciphertext)
	tampered[0] ^= 0xff
	w.Ciphertext = tampered

	_, err = UnwrapIdentity(w, password)
	if err == nil {
		t.Fatal("UnwrapIdentity with tampered ciphertext: expected error, got nil")
	}
}

func TestUnwrapRejectsHostileArgonParameters(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	w, err := WrapIdentity(identity, "password", FastWrapParams)
	if err != nil {
		t.Fatal(err)
	}
	w.Memory = maxWrapMemory + 1
	if _, err := UnwrapIdentity(w, "password"); err == nil {
		t.Fatal("oversized memory cost accepted")
	}
	w.Memory = FastWrapParams.Memory
	w.Threads = maxWrapThreads + 1
	if _, err := UnwrapIdentity(w, "password"); err == nil {
		t.Fatal("oversized thread count accepted")
	}
}
