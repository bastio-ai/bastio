package encryption

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	svc, err := New("") // dev key
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	plaintext := "sensitive api key: sk-1234567890"
	customerID := "customer-001"

	enc, err := svc.Encrypt(plaintext, customerID, "")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if enc.Ciphertext == "" || enc.Nonce == "" {
		t.Fatal("ciphertext or nonce is empty")
	}
	if enc.Version != 1 {
		t.Errorf("expected version 1, got %d", enc.Version)
	}

	decrypted, err := svc.Decrypt(enc, customerID, "")
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestDifferentCustomersGetDifferentKeys(t *testing.T) {
	svc, err := New("")
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	enc1, err := svc.Encrypt("same text", "customer-a", "")
	if err != nil {
		t.Fatal(err)
	}

	enc2, err := svc.Encrypt("same text", "customer-b", "")
	if err != nil {
		t.Fatal(err)
	}

	if enc1.Ciphertext == enc2.Ciphertext {
		t.Error("different customers should produce different ciphertexts")
	}
}

func TestDecryptWrongCustomerFails(t *testing.T) {
	svc, err := New("")
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	enc, err := svc.Encrypt("secret", "customer-a", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Decrypt(enc, "customer-b", "")
	if err == nil {
		t.Error("expected decrypt with wrong customer to fail")
	}
}

func TestHashAPIKey(t *testing.T) {
	hash1 := HashAPIKey("sk-bastio-abc123")
	hash2 := HashAPIKey("sk-bastio-abc123")
	hash3 := HashAPIKey("sk-bastio-xyz789")

	if hash1 != hash2 {
		t.Error("same key should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different keys should produce different hashes")
	}
	if len(hash1) == 0 {
		t.Error("hash should not be empty")
	}
}
