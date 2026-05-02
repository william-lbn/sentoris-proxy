package audit

import (
	"testing"
)

func TestEd25519Signer_GenerateKeyPair(t *testing.T) {
	publicKey, privateKey, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	if len(publicKey) != 32 {
		t.Errorf("Expected public key length 32, got %d", len(publicKey))
	}

	if len(privateKey) != 64 {
		t.Errorf("Expected private key length 64, got %d", len(privateKey))
	}
}

func TestEd25519Signer_SignAndVerify(t *testing.T) {
	_, privateKey, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	signer, err := NewEd25519Signer(privateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	testData := map[string]interface{}{
		"trace_id": "test-trace-001",
		"model":    "gpt-4o",
		"version":  "1.0.0",
	}

	signature, err := signer.Sign(testData)
	if err != nil {
		t.Fatalf("Failed to sign data: %v", err)
	}

	if signature == "" {
		t.Error("Expected non-empty signature")
	}

	valid, err := signer.Verify(testData, signature)
	if err != nil {
		t.Fatalf("Failed to verify signature: %v", err)
	}

	if !valid {
		t.Error("Expected valid signature")
	}
}

func TestEd25519Signer_VerifyTamperedData(t *testing.T) {
	_, privateKey, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	signer, err := NewEd25519Signer(privateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	testData := map[string]interface{}{
		"trace_id": "test-trace-001",
		"model":    "gpt-4o",
	}

	signature, err := signer.Sign(testData)
	if err != nil {
		t.Fatalf("Failed to sign data: %v", err)
	}

	tamperedData := map[string]interface{}{
		"trace_id": "test-trace-001",
		"model":    "gpt-3.5-turbo",
	}

	valid, err := signer.Verify(tamperedData, signature)
	if err != nil {
		t.Fatalf("Failed to verify signature: %v", err)
	}

	if valid {
		t.Error("Expected invalid signature for tampered data")
	}
}

func TestEd25519Signer_GetPublicKey(t *testing.T) {
	_, privateKey, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	signer, err := NewEd25519Signer(privateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	publicKeyBase64 := signer.GetPublicKeyBase64()
	if publicKeyBase64 == "" {
		t.Error("Expected non-empty public key")
	}
}

func TestVerifyEd25519Signature(t *testing.T) {
	publicKey, privateKey, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	signer, err := NewEd25519Signer(privateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	testData := map[string]interface{}{
		"test": "data",
	}

	signature, err := signer.Sign(testData)
	if err != nil {
		t.Fatalf("Failed to sign data: %v", err)
	}

	publicKeyBase64 := signer.GetPublicKeyBase64()

	valid, err := VerifyEd25519Signature(publicKeyBase64, testData, signature)
	if err != nil {
		t.Fatalf("Failed to verify signature: %v", err)
	}

	if !valid {
		t.Error("Expected valid signature")
	}

	_ = publicKey
}

func TestEd25519Signer_InvalidKeySize(t *testing.T) {
	invalidKey := make([]byte, 32)

	_, err := NewEd25519Signer(invalidKey)
	if err == nil {
		t.Error("Expected error for invalid key size")
	}
}
