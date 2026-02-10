package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestSSHSignatureVerification_TrustedKey verifies that a commit signed with a key
// in allowed_signers is verified as Valid=true with TrustFull.
func TestSSHSignatureVerification_TrustedKey(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	require.NoError(t, err)

	// Create a test commit
	commit := createTestCommit(t, "Alice", "alice@example.com")

	// Sign the commit
	signedCommit, err := signCommit(commit, signer)
	require.NoError(t, err)

	// Create verifier with the trusted key
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"alice@example.com": sshPubKey,
	})

	// Verify the signature
	result, err := signedCommit.VerifySignature(verifier)
	require.NoError(t, err)

	// Assert results
	assert.True(t, result.Valid, "expected Valid=true, got false with error: %v", result.Error)
	assert.Equal(t, object.TrustFull, result.TrustLevel)
	assert.Equal(t, "alice@example.com", result.Signer)
	assert.Equal(t, object.SignatureTypeSSH, result.Type)
	assert.NotEmpty(t, result.KeyID)
	assert.True(t, result.IsValid())
	assert.True(t, result.IsTrusted(object.TrustFull))
}

// TestSSHSignatureVerification_UntrustedKey verifies that a commit signed with a key
// NOT in allowed_signers is verified as Valid=true with TrustUndefined.
func TestSSHSignatureVerification_UntrustedKey(t *testing.T) {
	t.Parallel()

	// Generate two different SSH key pairs
	// First key pair - will be used to sign
	_, privKey1, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPrivKey1, err := ssh.NewSignerFromKey(privKey1)
	require.NoError(t, err)

	// Second key pair - will be in allowed_signers (different from signing key)
	pubKey2, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPubKey2, err := ssh.NewPublicKey(pubKey2)
	require.NoError(t, err)

	// Create a signer with first key
	signer, err := NewSSHSigner(sshPrivKey1)
	require.NoError(t, err)

	// Create and sign a test commit
	commit := createTestCommit(t, "Bob", "bob@example.com")
	signedCommit, err := signCommit(commit, signer)
	require.NoError(t, err)

	// Create verifier with only the second key (different from signing key)
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"alice@example.com": sshPubKey2,
	})

	// Verify the signature
	result, err := signedCommit.VerifySignature(verifier)
	require.NoError(t, err)

	// Assert results - signature is cryptographically valid but not trusted
	assert.True(t, result.Valid, "expected Valid=true (signature is cryptographically valid), got false with error: %v", result.Error)
	assert.Equal(t, object.TrustUndefined, result.TrustLevel)
	assert.Empty(t, result.Signer, "expected empty Signer for untrusted key")
	assert.NotEmpty(t, result.KeyID, "expected non-empty KeyID even for untrusted signatures")
	assert.True(t, result.IsValid(), "expected IsValid()=true (signature is cryptographically valid)")
	assert.False(t, result.IsTrusted(object.TrustFull), "expected IsTrusted(TrustFull)=false for untrusted key")

	// Note: IsTrusted(TrustUndefined) returns true because TrustUndefined is the minimum
	// trust level (0) and the signature is cryptographically valid. This is expected behavior:
	// the signature is valid but not from a trusted key.
	assert.True(t, result.IsTrusted(object.TrustUndefined), "expected IsTrusted(TrustUndefined)=true (valid signature, even if not trusted)")

	// Should require at least TrustMarginal to be considered trusted
	assert.False(t, result.IsTrusted(object.TrustMarginal), "expected IsTrusted(TrustMarginal)=false for untrusted key")
}

// TestSSHSignatureVerification_UnsignedCommit verifies that attempting to verify
// an unsigned commit returns appropriate error.
func TestSSHSignatureVerification_UnsignedCommit(t *testing.T) {
	t.Parallel()

	// Create an unsigned commit
	commit := createTestCommit(t, "Charlie", "charlie@example.com")

	// Verify commit has no signature
	assert.Empty(t, commit.PGPSignature, "test commit should not have a signature")

	// Create a verifier
	verifier := NewSSHVerifier(nil)

	// Attempt to verify the unsigned commit
	result, err := commit.VerifySignature(verifier)

	// Should return ErrNoSignature
	assert.ErrorIs(t, err, object.ErrNoSignature)
	assert.Nil(t, result, "expected nil result for unsigned commit")
}

// TestSSHSignatureVerification_InvalidSignature verifies that a tampered signature
// is detected and reported as Valid=false.
func TestSSHSignatureVerification_InvalidSignature(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	require.NoError(t, err)

	// Create and sign a test commit
	commit := createTestCommit(t, "Dave", "dave@example.com")
	signedCommit, err := signCommit(commit, signer)
	require.NoError(t, err)

	// Tamper with the signature by modifying a byte in the middle
	originalSig := signedCommit.PGPSignature
	require.GreaterOrEqual(t, len(originalSig), 100, "signature too short to tamper with safely")

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
	require.NoError(t, err)

	// Assert results - signature should be invalid
	assert.False(t, result.Valid, "expected Valid=false for tampered signature")
	assert.NotNil(t, result.Error, "expected non-nil Error for tampered signature")
	assert.False(t, result.IsValid(), "expected IsValid()=false for tampered signature")
	assert.False(t, result.IsTrusted(object.TrustFull), "expected IsTrusted(TrustFull)=false for tampered signature")
}

// TestSSHSignatureVerification_ConfigBasedFlow verifies the full workflow of
// loading a verifier from config and verifying a commit.
func TestSSHSignatureVerification_ConfigBasedFlow(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

	// Create allowed_signers file
	tmpDir := t.TempDir()
	allowedSignersPath := tmpDir + "/allowed_signers"

	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPubKey))
	content := "eve@example.com " + authorizedKey

	err = writeFile(allowedSignersPath, []byte(content), 0o600)
	require.NoError(t, err)

	// Create config with allowed signers file path
	cfg := config.NewConfig()
	cfg.GPG.SSH.AllowedSignersFile = allowedSignersPath

	// Create verifier from config
	verifier, err := NewSSHVerifierFromConfig(cfg)
	require.NoError(t, err)

	require.NotNil(t, verifier, "expected non-nil verifier from config")

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	require.NoError(t, err)

	// Create and sign a test commit
	commit := createTestCommit(t, "Eve", "eve@example.com")
	signedCommit, err := signCommit(commit, signer)
	require.NoError(t, err)

	// Verify the signature using config-based verifier
	result, err := signedCommit.VerifySignature(verifier)
	require.NoError(t, err)

	// Assert results
	assert.True(t, result.Valid, "expected Valid=true, got false with error: %v", result.Error)
	assert.Equal(t, object.TrustFull, result.TrustLevel)
	assert.Equal(t, "eve@example.com", result.Signer)
	assert.True(t, result.IsTrusted(object.TrustFull))
}

// TestSSHSignatureVerification_ConfigWithoutAllowedSigners verifies that
// creating a verifier from config without allowed_signers file returns nil verifier.
func TestSSHSignatureVerification_ConfigWithoutAllowedSigners(t *testing.T) {
	t.Parallel()

	// Create config without allowed signers file
	cfg := config.NewConfig()

	// Create verifier from config
	verifier, err := NewSSHVerifierFromConfig(cfg)
	require.NoError(t, err)

	assert.Nil(t, verifier, "expected nil verifier when no allowed signers file configured")
}

// TestSSHSignatureVerification_MultipleAllowedSigners verifies that verification
// works when multiple keys are in the allowed_signers list.
func TestSSHSignatureVerification_MultipleAllowedSigners(t *testing.T) {
	t.Parallel()

	// Generate three different SSH key pairs
	pubKey1, privKey1, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pubKey2, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pubKey3, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPrivKey1, err := ssh.NewSignerFromKey(privKey1)
	require.NoError(t, err)

	sshPubKey1, err := ssh.NewPublicKey(pubKey1)
	require.NoError(t, err)

	sshPubKey2, err := ssh.NewPublicKey(pubKey2)
	require.NoError(t, err)

	sshPubKey3, err := ssh.NewPublicKey(pubKey3)
	require.NoError(t, err)

	// Create verifier with multiple allowed signers
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"alice@example.com": sshPubKey1,
		"bob@example.com":   sshPubKey2,
		"carol@example.com": sshPubKey3,
	})

	// Create a signer with the first key
	signer, err := NewSSHSigner(sshPrivKey1)
	require.NoError(t, err)

	// Create and sign a test commit
	commit := createTestCommit(t, "Alice", "alice@example.com")
	signedCommit, err := signCommit(commit, signer)
	require.NoError(t, err)

	// Verify the signature
	result, err := signedCommit.VerifySignature(verifier)
	require.NoError(t, err)

	// Assert results - should match alice@example.com
	assert.True(t, result.Valid, "expected Valid=true, got false with error: %v", result.Error)
	assert.Equal(t, object.TrustFull, result.TrustLevel)
	assert.Equal(t, "alice@example.com", result.Signer)
}

// TestSSHSignatureVerification_NilVerifier verifies that passing nil verifier
// returns appropriate error.
func TestSSHSignatureVerification_NilVerifier(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	require.NoError(t, err)

	// Create a signer
	signer, err := NewSSHSigner(sshPrivKey)
	require.NoError(t, err)

	// Create and sign a test commit
	commit := createTestCommit(t, "Frank", "frank@example.com")
	signedCommit, err := signCommit(commit, signer)
	require.NoError(t, err)

	// Attempt to verify with nil verifier
	result, err := signedCommit.VerifySignature(nil)

	// Should return ErrNilVerifier
	assert.ErrorIs(t, err, object.ErrNilVerifier)
	assert.Nil(t, result, "expected nil result for nil verifier")
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
	require.NoError(t, err)

	// Should fail to parse as SSH signature
	assert.False(t, result.Valid, "expected Valid=false for non-SSH signature")
	assert.NotNil(t, result.Error, "expected error when parsing non-SSH signature")
}

// TestSSHSignatureVerification_WorktreeCommit verifies that the full worktree.Commit
// flow with SSH signing works correctly end-to-end.
func TestSSHSignatureVerification_WorktreeCommit(t *testing.T) {
	t.Parallel()

	// Generate a test SSH key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPrivKey, err := ssh.NewSignerFromKey(privKey)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

	// Create an SSHSigner
	signer, err := NewSSHSigner(sshPrivKey)
	require.NoError(t, err)

	// Create an in-memory repository
	repo, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	require.NoError(t, err)

	// Get the worktree
	w, err := repo.Worktree()
	require.NoError(t, err)

	// Create a commit with SSH signature using worktree.Commit
	commitHash, err := w.Commit("test commit with SSH signature", &CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now(),
		},
		Signer:            signer,
		AllowEmptyCommits: true,
	})
	require.NoError(t, err)

	// Retrieve the commit
	commit, err := repo.CommitObject(commitHash)
	require.NoError(t, err)

	// Verify the commit has a signature
	require.NotEmpty(t, commit.PGPSignature, "commit should have a signature")
	assert.Contains(t, commit.PGPSignature, "-----BEGIN SSH SIGNATURE-----")

	// Create a verifier with the signing key as trusted
	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"test@example.com": sshPubKey,
	})

	// Verify the signature
	result, err := commit.VerifySignature(verifier)
	require.NoError(t, err)

	// Assert results
	assert.True(t, result.Valid, "signature should be valid")
	assert.Equal(t, object.TrustFull, result.TrustLevel)
	assert.Equal(t, "test@example.com", result.Signer)
	assert.Equal(t, object.SignatureTypeSSH, result.Type)
	assert.NotEmpty(t, result.KeyID)
	assert.True(t, result.IsValid())
	assert.True(t, result.IsTrusted(object.TrustFull))
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
func signCommit(commit *object.Commit, signer Signer) (*object.Commit, error) {
	sig, err := signObject(signer, commit)
	if err != nil {
		return nil, err
	}

	signedCommit := *commit
	signedCommit.PGPSignature = string(sig)

	return &signedCommit, nil
}

// writeFile is a helper to write files.
func writeFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
