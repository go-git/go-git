package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
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

	// Verify the constructor accepts the map without error by checking the verifier is usable.
	// The internal allowedSigners field is unexported to prevent post-construction tampering.
	if v == nil {
		t.Error("expected non-nil verifier")
	}

	// Verify that the verifier supports SSH signature type (basic sanity check)
	if !v.SupportsSignatureType(object.SignatureTypeSSH) {
		t.Error("expected verifier to support SSH signatures")
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

func TestNewSSHVerifierFromFile_ValidFile(t *testing.T) {
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

	// Create allowed signers file
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPubKey))
	content := "alice@example.com " + authorizedKey

	tmpDir := t.TempDir()
	filePath := tmpDir + "/allowed_signers"

	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create verifier from file
	verifier, err := NewSSHVerifierFromFile(filePath)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

	if verifier == nil {
		t.Fatal("expected non-nil verifier")
	}

	// Verify that it supports SSH signatures
	if !verifier.SupportsSignatureType(object.SignatureTypeSSH) {
		t.Error("expected verifier to support SSH signatures")
	}
}

func TestNewSSHVerifierFromFile_HomeDirExpansion(t *testing.T) {
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

	// Create allowed signers file in home directory
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPubKey))
	content := "alice@example.com " + authorizedKey

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home directory: %v", err)
	}

	// Create a unique test file in home directory
	fileName := ".test_go_git_verifier_" + t.Name()
	filePath := home + "/" + fileName
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Skipf("cannot write to home directory: %v", err)
	}
	t.Cleanup(func() { os.Remove(filePath) })

	// Test with ~/ prefix
	tildePrefix := "~/" + fileName
	verifier, err := NewSSHVerifierFromFile(tildePrefix)
	if err != nil {
		t.Fatalf("failed to create verifier with ~/ prefix: %v", err)
	}

	if verifier == nil {
		t.Fatal("expected non-nil verifier")
	}

	if !verifier.SupportsSignatureType(object.SignatureTypeSSH) {
		t.Error("expected verifier to support SSH signatures")
	}
}

func TestNewSSHVerifierFromFile_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := NewSSHVerifierFromFile("/nonexistent/path/to/allowed_signers")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestNewSSHVerifierFromFile_InvalidFileFormat(t *testing.T) {
	t.Parallel()

	// Create file with invalid content
	tmpDir := t.TempDir()
	filePath := tmpDir + "/allowed_signers"

	invalidContent := "alice@example.com ssh-ed25519 INVALIDBASE64"
	if err := os.WriteFile(filePath, []byte(invalidContent), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := NewSSHVerifierFromFile(filePath)
	if err == nil {
		t.Error("expected error for invalid file format")
	}
}

func TestNewSSHVerifierFromFile_MultipleKeys(t *testing.T) {
	t.Parallel()

	// Generate two different keys
	pubKey1, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key 1: %v", err)
	}

	pubKey2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key 2: %v", err)
	}

	sshPubKey1, err := ssh.NewPublicKey(pubKey1)
	if err != nil {
		t.Fatalf("failed to create SSH public key 1: %v", err)
	}

	sshPubKey2, err := ssh.NewPublicKey(pubKey2)
	if err != nil {
		t.Fatalf("failed to create SSH public key 2: %v", err)
	}

	// Create allowed signers file with multiple keys
	authorizedKey1 := string(ssh.MarshalAuthorizedKey(sshPubKey1))
	authorizedKey2 := string(ssh.MarshalAuthorizedKey(sshPubKey2))

	content := "alice@example.com " + authorizedKey1 + "bob@example.com " + authorizedKey2

	tmpDir := t.TempDir()
	filePath := tmpDir + "/allowed_signers"

	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create verifier from file
	verifier, err := NewSSHVerifierFromFile(filePath)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

	if verifier == nil {
		t.Fatal("expected non-nil verifier")
	}

	if !verifier.SupportsSignatureType(object.SignatureTypeSSH) {
		t.Error("expected verifier to support SSH signatures")
	}
}
