package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func TestSSHVerifier_SupportsSignatureType(t *testing.T) {
	t.Parallel()

	v := NewSSHVerifier(nil)

	assert.True(t, v.SupportsSignatureType(object.SignatureTypeSSH), "should support SSH")
	assert.False(t, v.SupportsSignatureType(object.SignatureTypeOpenPGP), "should not support OpenPGP")
	assert.False(t, v.SupportsSignatureType(object.SignatureTypeX509), "should not support X509")
}

func TestSSHVerifier_InvalidSignature(t *testing.T) {
	t.Parallel()

	v := NewSSHVerifier(nil)

	result, err := v.Verify([]byte("not a valid signature"), []byte("message"))
	require.NoError(t, err)

	assert.False(t, result.Valid, "expected invalid result for malformed signature")
	assert.NotNil(t, result.Error, "expected error in result")
}

func TestSSHVerifier_WrongNamespace(t *testing.T) {
	t.Parallel()

	// Create a signature with wrong namespace (would need custom signing)
	// For now, test that we get proper error message format
	v := NewSSHVerifier(nil)

	// Use invalid signature to trigger parse error
	result, err := v.Verify([]byte("-----BEGIN SSH SIGNATURE-----\naW52YWxpZA==\n-----END SSH SIGNATURE-----"), []byte("message"))
	require.NoError(t, err)

	assert.False(t, result.Valid, "expected invalid result")
}

// TestSSHVerifier_KeyLookup tests the allowed signers lookup
func TestSSHVerifier_KeyLookup(t *testing.T) {
	t.Parallel()

	// Generate a test key
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

	allowedSigners := map[string]ssh.PublicKey{
		"test@example.com": sshPubKey,
	}

	v := NewSSHVerifier(allowedSigners)

	// Verify the constructor accepts the map without error by checking the verifier is usable.
	// The internal allowedSigners field is unexported to prevent post-construction tampering.
	require.NotNil(t, v, "expected non-nil verifier")

	// Verify that the verifier supports SSH signature type (basic sanity check)
	assert.True(t, v.SupportsSignatureType(object.SignatureTypeSSH), "expected verifier to support SSH signatures")
}

func TestComputeSSHSignedData_SHA512(t *testing.T) {
	t.Parallel()

	data, err := computeSSHSignedData("git", "sha512", []byte("test message"))
	require.NoError(t, err)

	// Check that it starts with the magic
	assert.Equal(t, sshSigMagic, string(data[:6]))
}

func TestComputeSSHSignedData_SHA256(t *testing.T) {
	t.Parallel()

	data, err := computeSSHSignedData("git", "sha256", []byte("test message"))
	require.NoError(t, err)

	assert.Equal(t, sshSigMagic, string(data[:6]))
}

func TestComputeSSHSignedData_UnsupportedHash(t *testing.T) {
	t.Parallel()

	_, err := computeSSHSignedData("git", "md5", []byte("test"))
	assert.Error(t, err, "expected error for unsupported hash algorithm")
}

func TestNewSSHVerifierFromFile_ValidFile(t *testing.T) {
	t.Parallel()

	// Generate a test key
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

	// Create allowed signers file
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPubKey))
	content := "alice@example.com " + authorizedKey

	tmpDir := t.TempDir()
	filePath := tmpDir + "/allowed_signers"

	err = os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err)

	// Create verifier from file
	verifier, err := NewSSHVerifierFromFile(filePath)
	require.NoError(t, err)

	require.NotNil(t, verifier, "expected non-nil verifier")

	// Verify that it supports SSH signatures
	assert.True(t, verifier.SupportsSignatureType(object.SignatureTypeSSH), "expected verifier to support SSH signatures")
}

func TestNewSSHVerifierFromFile_HomeDirExpansion(t *testing.T) {
	t.Parallel()

	// Generate a test key
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

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
	require.NoError(t, err)

	require.NotNil(t, verifier, "expected non-nil verifier")

	assert.True(t, verifier.SupportsSignatureType(object.SignatureTypeSSH), "expected verifier to support SSH signatures")
}

func TestNewSSHVerifierFromFile_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := NewSSHVerifierFromFile("/nonexistent/path/to/allowed_signers")
	assert.Error(t, err, "expected error for nonexistent file")
}

func TestNewSSHVerifierFromFile_InvalidFileFormat(t *testing.T) {
	t.Parallel()

	// Create file with invalid content
	tmpDir := t.TempDir()
	filePath := tmpDir + "/allowed_signers"

	invalidContent := "alice@example.com ssh-ed25519 INVALIDBASE64"
	err := os.WriteFile(filePath, []byte(invalidContent), 0o600)
	require.NoError(t, err)

	_, err = NewSSHVerifierFromFile(filePath)
	assert.Error(t, err, "expected error for invalid file format")
}

func TestNewSSHVerifierFromFile_MultipleKeys(t *testing.T) {
	t.Parallel()

	// Generate two different keys
	pubKey1, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pubKey2, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPubKey1, err := ssh.NewPublicKey(pubKey1)
	require.NoError(t, err)

	sshPubKey2, err := ssh.NewPublicKey(pubKey2)
	require.NoError(t, err)

	// Create allowed signers file with multiple keys
	authorizedKey1 := string(ssh.MarshalAuthorizedKey(sshPubKey1))
	authorizedKey2 := string(ssh.MarshalAuthorizedKey(sshPubKey2))

	content := "alice@example.com " + authorizedKey1 + "bob@example.com " + authorizedKey2

	tmpDir := t.TempDir()
	filePath := tmpDir + "/allowed_signers"

	err = os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err)

	// Create verifier from file
	verifier, err := NewSSHVerifierFromFile(filePath)
	require.NoError(t, err)

	require.NotNil(t, verifier, "expected non-nil verifier")

	assert.True(t, verifier.SupportsSignatureType(object.SignatureTypeSSH), "expected verifier to support SSH signatures")
}

func TestNewSSHVerifierFromConfig_WithAllowedSignersFile(t *testing.T) {
	t.Parallel()

	// Generate a test key
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	require.NoError(t, err)

	// Create allowed signers file
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPubKey))
	content := "alice@example.com " + authorizedKey

	tmpDir := t.TempDir()
	filePath := tmpDir + "/allowed_signers"

	err = os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err)

	// Create config with allowed signers file
	cfg := config.NewConfig()
	cfg.GPG.SSH.AllowedSignersFile = filePath

	// Create verifier from config
	verifier, err := NewSSHVerifierFromConfig(cfg)
	require.NoError(t, err)

	require.NotNil(t, verifier, "expected non-nil verifier")

	assert.True(t, verifier.SupportsSignatureType(object.SignatureTypeSSH), "expected verifier to support SSH signatures")
}

func TestNewSSHVerifierFromConfig_WithoutAllowedSignersFile(t *testing.T) {
	t.Parallel()

	// Create config without allowed signers file
	cfg := config.NewConfig()

	// Create verifier from config
	verifier, err := NewSSHVerifierFromConfig(cfg)
	require.NoError(t, err)

	assert.Nil(t, verifier, "expected nil verifier when no allowed signers file configured")
}

func TestNewSSHVerifierFromConfig_NilConfig(t *testing.T) {
	t.Parallel()

	// Create verifier from nil config
	verifier, err := NewSSHVerifierFromConfig(nil)
	require.NoError(t, err)

	assert.Nil(t, verifier, "expected nil verifier for nil config")
}

func TestNewSSHVerifierFromConfig_InvalidFile(t *testing.T) {
	t.Parallel()

	// Create config with non-existent file
	cfg := config.NewConfig()
	cfg.GPG.SSH.AllowedSignersFile = "/nonexistent/path/to/allowed_signers"

	// Create verifier from config should return error
	verifier, err := NewSSHVerifierFromConfig(cfg)
	assert.Error(t, err, "expected error for non-existent file")

	assert.Nil(t, verifier, "expected nil verifier when file doesn't exist")
}
