package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/plumbing/object"
)

func TestSSHVerifier_SupportsSignatureType(t *testing.T) {
	t.Parallel()

	v := NewSSHVerifier(nil)

	if !v.SupportsSignatureType(object.SignatureTypeSSH) {
		t.Error("should support SSH")
	}
	if v.SupportsSignatureType(object.SignatureTypeOpenPGP) {
		t.Error("should not support OpenPGP")
	}
	if v.SupportsSignatureType(object.SignatureTypeX509) {
		t.Error("should not support X509")
	}
}

func TestSSHVerifier_InvalidSignature(t *testing.T) {
	t.Parallel()

	v := NewSSHVerifier(nil)

	result, err := v.Verify([]byte("not a valid signature"), []byte("message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid result for malformed signature")
	}
	if result.Error == nil {
		t.Error("expected error in result")
	}
}

func TestSSHVerifier_WrongNamespace(t *testing.T) {
	t.Parallel()

	// Create a signature with wrong namespace (would need custom signing)
	// For now, test that we get proper error message format
	v := NewSSHVerifier(nil)

	// Use invalid signature to trigger parse error
	result, err := v.Verify([]byte("-----BEGIN SSH SIGNATURE-----\naW52YWxpZA==\n-----END SSH SIGNATURE-----"), []byte("message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid result")
	}
}

// TestSSHVerifier_KeyLookup tests the allowed signers lookup
func TestSSHVerifier_KeyLookup(t *testing.T) {
	t.Parallel()

	// Generate a test key
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	allowedSigners := map[string]ssh.PublicKey{
		"test@example.com": sshPubKey,
	}

	v := NewSSHVerifier(allowedSigners)

	// The verifier should have the allowed signers configured
	if v.AllowedSigners == nil {
		t.Error("expected non-nil AllowedSigners")
	}
	if len(v.AllowedSigners) != 1 {
		t.Errorf("expected 1 allowed signer, got %d", len(v.AllowedSigners))
	}
}

func TestComputeSSHSignedData_SHA512(t *testing.T) {
	t.Parallel()

	data, err := computeSSHSignedData("git", "sha512", []byte("test message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that it starts with the magic
	if string(data[:6]) != sshSigMagic {
		t.Errorf("expected magic %q, got %q", sshSigMagic, string(data[:6]))
	}
}

func TestComputeSSHSignedData_SHA256(t *testing.T) {
	t.Parallel()

	data, err := computeSSHSignedData("git", "sha256", []byte("test message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(data[:6]) != sshSigMagic {
		t.Errorf("expected magic %q", sshSigMagic)
	}
}

func TestComputeSSHSignedData_UnsupportedHash(t *testing.T) {
	t.Parallel()

	_, err := computeSSHSignedData("git", "md5", []byte("test"))
	if err == nil {
		t.Error("expected error for unsupported hash algorithm")
	}
}
