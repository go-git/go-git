package git

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/object"
)

// mockVerifier is a test helper that implements Verifier
type mockVerifier struct {
	supportedType object.SignatureType
	result        *object.VerificationResult
	err           error
}

func (m *mockVerifier) Verify(signature, message []byte) (*object.VerificationResult, error) {
	return m.result, m.err
}

func (m *mockVerifier) SupportsSignatureType(t object.SignatureType) bool {
	return t == m.supportedType
}

func TestVerifierChain_SelectsCorrectVerifier(t *testing.T) {
	t.Parallel()

	pgpVerifier := &mockVerifier{
		supportedType: object.SignatureTypeOpenPGP,
		result:        &object.VerificationResult{Valid: true, Type: object.SignatureTypeOpenPGP},
	}
	sshVerifier := &mockVerifier{
		supportedType: object.SignatureTypeSSH,
		result:        &object.VerificationResult{Valid: true, Type: object.SignatureTypeSSH},
	}

	chain := VerifierChain{pgpVerifier, sshVerifier}

	// Test PGP signature routes to PGP verifier
	pgpSig := []byte("-----BEGIN PGP SIGNATURE-----\ntest\n-----END PGP SIGNATURE-----")
	result, err := chain.Verify(pgpSig, []byte("message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != object.SignatureTypeOpenPGP {
		t.Errorf("expected OpenPGP type, got %v", result.Type)
	}

	// Test SSH signature routes to SSH verifier
	sshSig := []byte("-----BEGIN SSH SIGNATURE-----\ntest\n-----END SSH SIGNATURE-----")
	result, err = chain.Verify(sshSig, []byte("message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != object.SignatureTypeSSH {
		t.Errorf("expected SSH type, got %v", result.Type)
	}
}

func TestVerifierChain_NoMatchingVerifier(t *testing.T) {
	t.Parallel()

	// Chain with only SSH verifier
	chain := VerifierChain{
		&mockVerifier{supportedType: object.SignatureTypeSSH},
	}

	// Try to verify PGP signature
	pgpSig := []byte("-----BEGIN PGP SIGNATURE-----\ntest\n-----END PGP SIGNATURE-----")
	_, err := chain.Verify(pgpSig, []byte("message"))
	if !errors.Is(err, ErrNoVerifierForSignatureType) {
		t.Errorf("expected ErrNoVerifierForSignatureType, got %v", err)
	}
}

func TestVerifierChain_EmptyChain(t *testing.T) {
	t.Parallel()

	chain := VerifierChain{}

	sig := []byte("-----BEGIN PGP SIGNATURE-----\ntest\n-----END PGP SIGNATURE-----")
	_, err := chain.Verify(sig, []byte("message"))
	if !errors.Is(err, ErrNoVerifierForSignatureType) {
		t.Errorf("expected ErrNoVerifierForSignatureType, got %v", err)
	}
}

func TestVerifierChain_EmptySignature(t *testing.T) {
	t.Parallel()

	chain := VerifierChain{
		&mockVerifier{supportedType: object.SignatureTypeOpenPGP},
	}

	_, err := chain.Verify([]byte{}, []byte("message"))
	if !errors.Is(err, ErrNoSignature) {
		t.Errorf("expected ErrNoSignature, got %v", err)
	}

	_, err = chain.Verify(nil, []byte("message"))
	if !errors.Is(err, ErrNoSignature) {
		t.Errorf("expected ErrNoSignature for nil signature, got %v", err)
	}
}

func TestVerifierChain_SupportsSignatureType(t *testing.T) {
	t.Parallel()

	chain := VerifierChain{
		&mockVerifier{supportedType: object.SignatureTypeOpenPGP},
		&mockVerifier{supportedType: object.SignatureTypeSSH},
	}

	if !chain.SupportsSignatureType(object.SignatureTypeOpenPGP) {
		t.Error("chain should support OpenPGP")
	}
	if !chain.SupportsSignatureType(object.SignatureTypeSSH) {
		t.Error("chain should support SSH")
	}
	if chain.SupportsSignatureType(object.SignatureTypeX509) {
		t.Error("chain should not support X509")
	}
}

func TestVerifierChain_VerifierError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("verification failed")
	chain := VerifierChain{
		&mockVerifier{
			supportedType: object.SignatureTypeOpenPGP,
			err:           expectedErr,
		},
	}

	sig := []byte("-----BEGIN PGP SIGNATURE-----\ntest\n-----END PGP SIGNATURE-----")
	_, err := chain.Verify(sig, []byte("message"))
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected verifier error, got %v", err)
	}
}
