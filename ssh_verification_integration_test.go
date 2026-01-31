package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// TestSSHSignatureVerification_TrustedKey verifies that a commit signed with a key
// in allowed_signers is verified as Valid=true with TrustFull.
func TestSSHSignatureVerification_TrustedKey(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("failed to create SSH signer from private key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	if err != nil {
		t.Fatalf("failed to create SSHSigner: %v", err)
	}

	// Create a test commit
	commit := createTestCommit(t, "Alice", "alice@example.com")

	// Sign the commit
	signedCommit, err := signCommit(commit, signer)
	if err != nil {
		t.Fatalf("failed to sign commit: %v", err)
	}

	// Create verifier with the trusted key
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"alice@example.com": sshPubKey,
	})

	// Verify the signature
	result, err := signedCommit.VerifySignature(verifier)
	if err != nil {
		t.Fatalf("verification returned error: %v", err)
	}

	// Assert results
	if !result.Valid {
		t.Errorf("expected Valid=true, got false with error: %v", result.Error)
	}

	if result.TrustLevel != object.TrustFull {
		t.Errorf("expected TrustLevel=TrustFull, got %v", result.TrustLevel)
	}

	if result.Signer != "alice@example.com" {
		t.Errorf("expected Signer='alice@example.com', got %q", result.Signer)
	}

	if result.Type != object.SignatureTypeSSH {
		t.Errorf("expected Type=SignatureTypeSSH, got %v", result.Type)
	}

	if result.KeyID == "" {
		t.Error("expected non-empty KeyID")
	}

	if !result.IsValid() {
		t.Error("expected IsValid()=true")
	}

	if !result.IsTrusted(object.TrustFull) {
		t.Error("expected IsTrusted(TrustFull)=true")
	}
}

// TestSSHSignatureVerification_UntrustedKey verifies that a commit signed with a key
// NOT in allowed_signers is verified as Valid=true with TrustUndefined.
func TestSSHSignatureVerification_UntrustedKey(t *testing.T) {
	t.Parallel()

	// Generate two different SSH key pairs
	// First key pair - will be used to sign
	_, privKey1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	sshPrivKey1, err := ssh.NewSignerFromKey(privKey1)
	if err != nil {
		t.Fatalf("failed to create SSH signer from private key: %v", err)
	}

	// Second key pair - will be in allowed_signers (different from signing key)
	pubKey2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate allowed key: %v", err)
	}

	sshPubKey2, err := ssh.NewPublicKey(pubKey2)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	// Create a signer with first key
	signer, err := NewSSHSigner(sshPrivKey1)
	if err != nil {
		t.Fatalf("failed to create SSHSigner: %v", err)
	}

	// Create and sign a test commit
	commit := createTestCommit(t, "Bob", "bob@example.com")
	signedCommit, err := signCommit(commit, signer)
	if err != nil {
		t.Fatalf("failed to sign commit: %v", err)
	}

	// Create verifier with only the second key (different from signing key)
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"alice@example.com": sshPubKey2,
	})

	// Verify the signature
	result, err := signedCommit.VerifySignature(verifier)
	if err != nil {
		t.Fatalf("verification returned error: %v", err)
	}

	// Assert results - signature is cryptographically valid but not trusted
	if !result.Valid {
		t.Errorf("expected Valid=true (signature is cryptographically valid), got false with error: %v", result.Error)
	}

	if result.TrustLevel != object.TrustUndefined {
		t.Errorf("expected TrustLevel=TrustUndefined, got %v", result.TrustLevel)
	}

	if result.Signer != "" {
		t.Errorf("expected empty Signer for untrusted key, got %q", result.Signer)
	}

	if result.KeyID == "" {
		t.Error("expected non-empty KeyID even for untrusted signatures")
	}

	if !result.IsValid() {
		t.Error("expected IsValid()=true (signature is cryptographically valid)")
	}

	if result.IsTrusted(object.TrustFull) {
		t.Error("expected IsTrusted(TrustFull)=false for untrusted key")
	}

	// Note: IsTrusted(TrustUndefined) returns true because TrustUndefined is the minimum
	// trust level (0) and the signature is cryptographically valid. This is expected behavior:
	// the signature is valid but not from a trusted key.
	if !result.IsTrusted(object.TrustUndefined) {
		t.Error("expected IsTrusted(TrustUndefined)=true (valid signature, even if not trusted)")
	}

	// Should require at least TrustMarginal to be considered trusted
	if result.IsTrusted(object.TrustMarginal) {
		t.Error("expected IsTrusted(TrustMarginal)=false for untrusted key")
	}
}

// TestSSHSignatureVerification_UnsignedCommit verifies that attempting to verify
// an unsigned commit returns appropriate error.
func TestSSHSignatureVerification_UnsignedCommit(t *testing.T) {
	t.Parallel()

	// Create an unsigned commit
	commit := createTestCommit(t, "Charlie", "charlie@example.com")

	// Verify commit has no signature
	if commit.PGPSignature != "" {
		t.Fatal("test commit should not have a signature")
	}

	// Create a verifier
	verifier := NewSSHVerifier(nil)

	// Attempt to verify the unsigned commit
	result, err := commit.VerifySignature(verifier)

	// Should return ErrNoSignature
	if !errors.Is(err, object.ErrNoSignature) {
		t.Errorf("expected ErrNoSignature, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil result for unsigned commit, got %+v", result)
	}
}

// TestSSHSignatureVerification_InvalidSignature verifies that a tampered signature
// is detected and reported as Valid=false.
func TestSSHSignatureVerification_InvalidSignature(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("failed to create SSH signer from private key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	if err != nil {
		t.Fatalf("failed to create SSHSigner: %v", err)
	}

	// Create and sign a test commit
	commit := createTestCommit(t, "Dave", "dave@example.com")
	signedCommit, err := signCommit(commit, signer)
	if err != nil {
		t.Fatalf("failed to sign commit: %v", err)
	}

	// Tamper with the signature by modifying a byte in the middle
	originalSig := signedCommit.PGPSignature
	if len(originalSig) < 100 {
		t.Fatal("signature too short to tamper with safely")
	}

	// Modify a character in the base64 encoded part (not the armor headers)
	tamperedSig := []byte(originalSig)
	// Find a position after the header but before the footer
	tamperPos := len("-----BEGIN SSH SIGNATURE-----\n") + 50
	if tamperedSig[tamperPos] == 'A' {
		tamperedSig[tamperPos] = 'B'
	} else {
		tamperedSig[tamperPos] = 'A'
	}
	signedCommit.PGPSignature = string(tamperedSig)

	// Create verifier with the trusted key
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"dave@example.com": sshPubKey,
	})

	// Verify the tampered signature
	result, err := signedCommit.VerifySignature(verifier)
	if err != nil {
		t.Fatalf("verification returned error: %v", err)
	}

	// Assert results - signature should be invalid
	if result.Valid {
		t.Error("expected Valid=false for tampered signature")
	}

	if result.Error == nil {
		t.Error("expected non-nil Error for tampered signature")
	}

	if result.IsValid() {
		t.Error("expected IsValid()=false for tampered signature")
	}

	if result.IsTrusted(object.TrustFull) {
		t.Error("expected IsTrusted(TrustFull)=false for tampered signature")
	}
}

// TestSSHSignatureVerification_ConfigBasedFlow verifies the full workflow of
// loading a verifier from config and verifying a commit.
func TestSSHSignatureVerification_ConfigBasedFlow(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("failed to create SSH signer from private key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	// Create allowed_signers file
	tmpDir := t.TempDir()
	allowedSignersPath := tmpDir + "/allowed_signers"

	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPubKey))
	content := "eve@example.com " + authorizedKey

	if err := writeFile(allowedSignersPath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write allowed_signers file: %v", err)
	}

	// Create config with allowed signers file path
	cfg := config.NewConfig()
	cfg.GPG.SSH.AllowedSignersFile = allowedSignersPath

	// Create verifier from config
	verifier, err := NewSSHVerifierFromConfig(cfg)
	if err != nil {
		t.Fatalf("failed to create verifier from config: %v", err)
	}

	if verifier == nil {
		t.Fatal("expected non-nil verifier from config")
	}

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	if err != nil {
		t.Fatalf("failed to create SSHSigner: %v", err)
	}

	// Create and sign a test commit
	commit := createTestCommit(t, "Eve", "eve@example.com")
	signedCommit, err := signCommit(commit, signer)
	if err != nil {
		t.Fatalf("failed to sign commit: %v", err)
	}

	// Verify the signature using config-based verifier
	result, err := signedCommit.VerifySignature(verifier)
	if err != nil {
		t.Fatalf("verification returned error: %v", err)
	}

	// Assert results
	if !result.Valid {
		t.Errorf("expected Valid=true, got false with error: %v", result.Error)
	}

	if result.TrustLevel != object.TrustFull {
		t.Errorf("expected TrustLevel=TrustFull, got %v", result.TrustLevel)
	}

	if result.Signer != "eve@example.com" {
		t.Errorf("expected Signer='eve@example.com', got %q", result.Signer)
	}

	if !result.IsTrusted(object.TrustFull) {
		t.Error("expected IsTrusted(TrustFull)=true")
	}
}

// TestSSHSignatureVerification_ConfigWithoutAllowedSigners verifies that
// creating a verifier from config without allowed_signers file returns nil verifier.
func TestSSHSignatureVerification_ConfigWithoutAllowedSigners(t *testing.T) {
	t.Parallel()

	// Create config without allowed signers file
	cfg := config.NewConfig()

	// Create verifier from config
	verifier, err := NewSSHVerifierFromConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if verifier != nil {
		t.Error("expected nil verifier when no allowed signers file configured")
	}
}

// TestSSHSignatureVerification_MultipleAllowedSigners verifies that verification
// works when multiple keys are in the allowed_signers list.
func TestSSHSignatureVerification_MultipleAllowedSigners(t *testing.T) {
	t.Parallel()

	// Generate three different SSH key pairs
	pubKey1, privKey1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key 1: %v", err)
	}

	pubKey2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key 2: %v", err)
	}

	pubKey3, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key 3: %v", err)
	}

	sshPrivKey1, err := ssh.NewSignerFromKey(privKey1)
	if err != nil {
		t.Fatalf("failed to create SSH signer from private key: %v", err)
	}

	sshPubKey1, err := ssh.NewPublicKey(pubKey1)
	if err != nil {
		t.Fatalf("failed to create SSH public key 1: %v", err)
	}

	sshPubKey2, err := ssh.NewPublicKey(pubKey2)
	if err != nil {
		t.Fatalf("failed to create SSH public key 2: %v", err)
	}

	sshPubKey3, err := ssh.NewPublicKey(pubKey3)
	if err != nil {
		t.Fatalf("failed to create SSH public key 3: %v", err)
	}

	// Create verifier with multiple allowed signers
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"alice@example.com": sshPubKey1,
		"bob@example.com":   sshPubKey2,
		"carol@example.com": sshPubKey3,
	})

	// Create a signer with the first key
	signer, err := NewSSHSigner(sshPrivKey1)
	if err != nil {
		t.Fatalf("failed to create SSHSigner: %v", err)
	}

	// Create and sign a test commit
	commit := createTestCommit(t, "Alice", "alice@example.com")
	signedCommit, err := signCommit(commit, signer)
	if err != nil {
		t.Fatalf("failed to sign commit: %v", err)
	}

	// Verify the signature
	result, err := signedCommit.VerifySignature(verifier)
	if err != nil {
		t.Fatalf("verification returned error: %v", err)
	}

	// Assert results - should match alice@example.com
	if !result.Valid {
		t.Errorf("expected Valid=true, got false with error: %v", result.Error)
	}

	if result.TrustLevel != object.TrustFull {
		t.Errorf("expected TrustLevel=TrustFull, got %v", result.TrustLevel)
	}

	if result.Signer != "alice@example.com" {
		t.Errorf("expected Signer='alice@example.com', got %q", result.Signer)
	}
}

// TestSSHSignatureVerification_NilVerifier verifies that passing nil verifier
// returns appropriate error.
func TestSSHSignatureVerification_NilVerifier(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("failed to create SSH signer from private key: %v", err)
	}

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	if err != nil {
		t.Fatalf("failed to create SSHSigner: %v", err)
	}

	// Create and sign a test commit
	commit := createTestCommit(t, "Frank", "frank@example.com")
	signedCommit, err := signCommit(commit, signer)
	if err != nil {
		t.Fatalf("failed to sign commit: %v", err)
	}

	// Attempt to verify with nil verifier
	result, err := signedCommit.VerifySignature(nil)

	// Should return ErrNilVerifier
	if !errors.Is(err, object.ErrNilVerifier) {
		t.Errorf("expected ErrNilVerifier, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil result for nil verifier, got %+v", result)
	}
}

// TestSSHSignatureVerification_WrongSignatureType verifies that a commit with
// an OpenPGP signature cannot be verified by SSH verifier.
func TestSSHSignatureVerification_WrongSignatureType(t *testing.T) {
	t.Parallel()

	// Create a commit with a fake OpenPGP signature (not a real signature, just testing detection)
	commit := createTestCommit(t, "Grace", "grace@example.com")
	commit.PGPSignature = "-----BEGIN PGP SIGNATURE-----\nfake\n-----END PGP SIGNATURE-----"

	// Create SSH verifier
	verifier := NewSSHVerifier(nil)

	// Attempt to verify
	result, err := verifier.Verify([]byte(commit.PGPSignature), []byte("message"))
	if err != nil {
		t.Fatalf("verification returned error: %v", err)
	}

	// Should fail to parse as SSH signature
	if result.Valid {
		t.Error("expected Valid=false for non-SSH signature")
	}

	if result.Error == nil {
		t.Error("expected error when parsing non-SSH signature")
	}
}

// Helper functions

// createTestCommit creates a minimal commit object for testing.
func createTestCommit(t *testing.T, name, email string) *object.Commit {
	t.Helper()

	return &object.Commit{
		Hash: plumbing.ZeroHash,
		Author: object.Signature{
			Name:  name,
			Email: email,
			When:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		Committer: object.Signature{
			Name:  name,
			Email: email,
			When:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		Message:  "Test commit message",
		TreeHash: plumbing.ZeroHash,
	}
}

// signCommit signs a commit using the provided signer and returns a new commit
// with the signature attached.
func signCommit(commit *object.Commit, signer *SSHSigner) (*object.Commit, error) {
	// Encode commit without signature
	encoded := &plumbing.MemoryObject{}
	if err := commit.EncodeWithoutSignature(encoded); err != nil {
		return nil, err
	}

	reader, err := encoded.Reader()
	if err != nil {
		return nil, err
	}

	// Sign the encoded commit
	signature, err := signer.Sign(reader)
	if err != nil {
		return nil, err
	}

	// Create a new commit with the signature
	signedCommit := *commit
	signedCommit.PGPSignature = string(signature)

	return &signedCommit, nil
}

// writeFile is a helper to write files.
func writeFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
