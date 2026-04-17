package crypto

import (
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// 32-byte key encoded as 64 hex chars.
	t.Setenv("MULTICA_INTEGRATION_KEY", "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")

	plain := "hello"
	encrypted, err := EncryptToken(plain)
	if err != nil {
		t.Fatalf("EncryptToken: %v", err)
	}
	if encrypted == plain {
		t.Fatal("encrypted value should not equal plaintext")
	}

	decrypted, err := DecryptToken(encrypted)
	if err != nil {
		t.Fatalf("DecryptToken: %v", err)
	}
	if decrypted != plain {
		t.Errorf("round-trip: got %q, want %q", decrypted, plain)
	}
}

func TestEncryptToken_MissingKey(t *testing.T) {
	t.Setenv("MULTICA_INTEGRATION_KEY", "")

	_, err := EncryptToken("hello")
	if err != ErrMissingKey {
		t.Errorf("expected ErrMissingKey, got %v", err)
	}
}

func TestEncryptToken_BadKey(t *testing.T) {
	t.Setenv("MULTICA_INTEGRATION_KEY", "notvalidhex")

	_, err := EncryptToken("hello")
	if err == nil {
		t.Fatal("expected error for bad key")
	}
}

func TestDecryptToken_Tampered(t *testing.T) {
	t.Setenv("MULTICA_INTEGRATION_KEY", "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")

	encrypted, err := EncryptToken("secret")
	if err != nil {
		t.Fatalf("EncryptToken: %v", err)
	}

	// Tamper by appending extra bytes (the base64 decodes to a different ciphertext).
	tampered := encrypted + "AAAA"
	_, err = DecryptToken(tampered)
	if err == nil {
		t.Fatal("expected error decrypting tampered ciphertext")
	}
}
