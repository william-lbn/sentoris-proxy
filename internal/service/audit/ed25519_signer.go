package audit

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
	"github.com/sentoris-ai/sentoris-proxy/pkg/jcs"
)

type Ed25519Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

func NewEd25519Signer(privateKey []byte) (*Ed25519Signer, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: expected %d, got %d", ed25519.PrivateKeySize, len(privateKey))
	}

	return &Ed25519Signer{
		privateKey: ed25519.PrivateKey(privateKey),
		publicKey:  ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey),
	}, nil
}

func GenerateEd25519KeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

func (s *Ed25519Signer) Sign(data any) (string, error) {
	canonical, err := jcs.Canonicalize(data)
	if err != nil {
		return "", fmt.Errorf("canonicalization failed: %w", err)
	}

	signature := ed25519.Sign(s.privateKey, canonical)
	return base64.StdEncoding.EncodeToString(signature), nil
}

func (s *Ed25519Signer) SignTrace(trace *domain.Trace) (string, error) {
	if trace == nil {
		return "", fmt.Errorf("trace cannot be nil")
	}

	traceCopy := *trace
	traceCopy.Proofs.AuditSignature = ""

	traceMap := traceToMap(&traceCopy)
	stripped := jcs.StripNonSignatureFields(traceMap)
	return s.Sign(stripped)
}

func (s *Ed25519Signer) Verify(data any, signatureBase64 string) (bool, error) {
	canonical, err := jcs.Canonicalize(data)
	if err != nil {
		return false, fmt.Errorf("canonicalization failed: %w", err)
	}

	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false, fmt.Errorf("invalid signature format: %w", err)
	}

	return ed25519.Verify(s.publicKey, canonical, signature), nil
}

func (s *Ed25519Signer) GetPublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(s.publicKey)
}

func VerifyEd25519Signature(publicKeyBase64 string, data any, signatureBase64 string) (bool, error) {
	publicKey, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil {
		return false, fmt.Errorf("invalid public key format: %w", err)
	}

	if len(publicKey) != ed25519.PublicKeySize {
		return false, fmt.Errorf("invalid public key size: expected %d, got %d", ed25519.PublicKeySize, len(publicKey))
	}

	canonical, err := jcs.Canonicalize(data)
	if err != nil {
		return false, fmt.Errorf("canonicalization failed: %w", err)
	}

	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false, fmt.Errorf("invalid signature format: %w", err)
	}

	return ed25519.Verify(ed25519.PublicKey(publicKey), canonical, signature), nil
}

type Ed25519Proof struct {
	ProofType     string `json:"proof_type"`
	PublicKey     string `json:"public_key"`
	Signature     string `json:"signature"`
	CanonicalData string `json:"canonical_data,omitempty"`
}

func (p *Ed25519Proof) ToMap() (map[string]interface{}, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
