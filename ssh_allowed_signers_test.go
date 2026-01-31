package git

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestParseAllowedSigners_SinglePrincipal_ED25519(t *testing.T) {
	t.Parallel()

	// Generate an ed25519 key
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	// Create allowed signers content
	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_SinglePrincipal_RSA(t *testing.T) {
	t.Parallel()

	// Generate an RSA key
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_SinglePrincipal_ECDSA(t *testing.T) {
	t.Parallel()

	// Generate an ECDSA key
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_MultiplePrincipals(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "alice@example.com,bob@example.com,charlie@example.com " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 3 {
		t.Fatalf("expected 3 signers, got %d", len(signers))
	}

	expectedPrincipals := []string{"alice@example.com", "bob@example.com", "charlie@example.com"}
	for _, principal := range expectedPrincipals {
		key, ok := signers[principal]
		if !ok {
			t.Errorf("expected %s in signers", principal)
			continue
		}

		if !sshKeysEqual(key, sshPubKey) {
			t.Errorf("key for %s does not match", principal)
		}
	}
}

func TestParseAllowedSigners_WildcardPrincipal(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "* " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["*"]
	if !ok {
		t.Fatal("expected * in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_CommentsAndEmptyLines(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := `# This is a comment
# Another comment

user@example.com ` + authorizedKey + `

# Final comment
`

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_WithComment(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com " + authorizedKey + " this is a comment"

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_WithNamespaces(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com namespaces=\"git\" " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_WithValidAfter(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com valid-after=\"20230101\" " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_WithValidBefore(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com valid-before=\"20251231\" " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_WithMultipleOptions(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com namespaces=\"git\" valid-after=\"20230101\" valid-before=\"20251231\" " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_MultipleKeys(t *testing.T) {
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

	authorizedKey1 := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey1)))
	authorizedKey2 := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey2)))

	content := `alice@example.com ` + authorizedKey1 + `
bob@example.com ` + authorizedKey2

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 2 {
		t.Fatalf("expected 2 signers, got %d", len(signers))
	}

	key1, ok := signers["alice@example.com"]
	if !ok {
		t.Fatal("expected alice@example.com in signers")
	}

	if !sshKeysEqual(key1, sshPubKey1) {
		t.Error("key for alice@example.com does not match")
	}

	key2, ok := signers["bob@example.com"]
	if !ok {
		t.Fatal("expected bob@example.com in signers")
	}

	if !sshKeysEqual(key2, sshPubKey2) {
		t.Error("key for bob@example.com does not match")
	}
}

func TestParseAllowedSigners_InvalidFormat_NoPrincipal(t *testing.T) {
	t.Parallel()

	content := ""

	_, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Errorf("empty file should not error, got: %v", err)
	}
}

func TestParseAllowedSigners_InvalidFormat_NoKey(t *testing.T) {
	t.Parallel()

	content := "user@example.com"

	_, err := ParseAllowedSigners(strings.NewReader(content))
	if err == nil {
		t.Error("expected error for line without key")
	}
}

func TestParseAllowedSigners_InvalidFormat_InvalidKey(t *testing.T) {
	t.Parallel()

	content := "user@example.com ssh-ed25519 INVALIDBASE64"

	_, err := ParseAllowedSigners(strings.NewReader(content))
	if err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestParseAllowedSignersFile_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := ParseAllowedSignersFile("/nonexistent/path/to/allowed_signers")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseAllowedSignersFile_ValidFile(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com " + authorizedKey

	// Create a temporary file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "allowed_signers")

	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	signers, err := ParseAllowedSignersFile(filePath)
	if err != nil {
		t.Fatalf("failed to parse file: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSignersFile_HomeDirExpansion(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com " + authorizedKey

	// Create a file in a temp directory and construct a path with ~/
	tmpDir := t.TempDir()
	relPath := "test_allowed_signers"
	filePath := filepath.Join(tmpDir, relPath)

	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Get the home directory
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home directory: %v", err)
	}

	// Create a symlink or use a real path under home
	// For testing, we'll just verify that the expansion logic works by creating a file there
	homeTestPath := filepath.Join(home, ".test_go_git_allowed_signers_"+t.Name())
	if err := os.WriteFile(homeTestPath, []byte(content), 0o600); err != nil {
		t.Skipf("cannot write to home directory: %v", err)
	}
	t.Cleanup(func() { os.Remove(homeTestPath) })

	tildePrefix := "~/" + filepath.Base(homeTestPath)
	signers, err := ParseAllowedSignersFile(tildePrefix)
	if err != nil {
		t.Fatalf("failed to parse file with ~/ prefix: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_ComplexRealWorldExample(t *testing.T) {
	t.Parallel()

	// Generate test keys
	pubKey1, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key 1: %v", err)
	}

	pubKey2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key 2: %v", err)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	sshPubKey1, err := ssh.NewPublicKey(pubKey1)
	if err != nil {
		t.Fatalf("failed to create SSH public key 1: %v", err)
	}

	sshPubKey2, err := ssh.NewPublicKey(pubKey2)
	if err != nil {
		t.Fatalf("failed to create SSH public key 2: %v", err)
	}

	sshRSAKey, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatalf("failed to create SSH RSA key: %v", err)
	}

	authorizedKey1 := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey1)))
	authorizedKey2 := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey2)))
	authorizedRSAKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshRSAKey)))

	content := `# Allowed signers for my repository
# Updated 2025-01-31

# Development team
alice@example.com ` + authorizedKey1 + ` Alice's signing key
bob@example.com,bob@work.com namespaces="git" ` + authorizedKey2 + `

# Release signing key
release@example.com valid-after="20250101" ` + authorizedRSAKey + `

# Wildcard for testing
* namespaces="git" valid-before="20251231" ` + authorizedKey1 + `
`

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should have 5 entries: alice, bob, bob@work.com, release, *
	if len(signers) != 5 {
		t.Fatalf("expected 5 signers, got %d", len(signers))
	}

	// Verify each entry
	tests := []struct {
		principal string
		key       ssh.PublicKey
	}{
		{"alice@example.com", sshPubKey1},
		{"bob@example.com", sshPubKey2},
		{"bob@work.com", sshPubKey2},
		{"release@example.com", sshRSAKey},
		{"*", sshPubKey1},
	}

	for _, tt := range tests {
		key, ok := signers[tt.principal]
		if !ok {
			t.Errorf("expected %s in signers", tt.principal)
			continue
		}

		if !sshKeysEqual(key, tt.key) {
			t.Errorf("key for %s does not match", tt.principal)
		}
	}
}

func TestParseAllowedSigners_TrailingWhitespace(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	content := "user@example.com " + authorizedKey + "   \t  \n"

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	key, ok := signers["user@example.com"]
	if !ok {
		t.Fatal("expected user@example.com in signers")
	}

	if !sshKeysEqual(key, sshPubKey) {
		t.Error("keys do not match")
	}
}

func TestParseAllowedSigners_EmptyPrincipalInList(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	// Test with empty entries in comma-separated list
	content := "alice@example.com,,bob@example.com " + authorizedKey

	signers, err := ParseAllowedSigners(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should have 2 signers (empty entries are skipped)
	if len(signers) != 2 {
		t.Fatalf("expected 2 signers, got %d", len(signers))
	}

	for _, principal := range []string{"alice@example.com", "bob@example.com"} {
		key, ok := signers[principal]
		if !ok {
			t.Errorf("expected %s in signers", principal)
			continue
		}

		if !sshKeysEqual(key, sshPubKey) {
			t.Errorf("key for %s does not match", principal)
		}
	}
}

func TestParseAllowedSigners_DuplicatePrincipal(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))

	// Test duplicate principal on separate lines
	content := `user@example.com ` + authorizedKey + `
user@example.com ` + authorizedKey

	_, err = ParseAllowedSigners(strings.NewReader(content))
	if err == nil {
		t.Error("expected error for duplicate principal")
	}
	if err != nil && !strings.Contains(err.Error(), "duplicate principal") {
		t.Errorf("expected duplicate principal error, got: %v", err)
	}
}

func TestParseAllowedSigners_DuplicatePrincipalInSameLine(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))

	// Test duplicate principal in comma-separated list on same line
	content := "user@example.com,alice@example.com,user@example.com " + authorizedKey

	_, err = ParseAllowedSigners(strings.NewReader(content))
	if err == nil {
		t.Error("expected error for duplicate principal")
	}
	if err != nil && !strings.Contains(err.Error(), "duplicate principal") {
		t.Errorf("expected duplicate principal error, got: %v", err)
	}
}

// Benchmark parsing performance
func BenchmarkParseAllowedSigners(b *testing.B) {
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		b.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))

	// Create a large file with many entries
	var buf bytes.Buffer
	for i := range 100 {
		fmt.Fprintf(&buf, "user%d@example.com ", i)
		buf.WriteString(authorizedKey)
		buf.WriteByte('\n')
	}
	content := buf.Bytes()

	b.ResetTimer()
	for b.Loop() {
		_, err := ParseAllowedSigners(bytes.NewReader(content))
		if err != nil {
			b.Fatalf("failed to parse: %v", err)
		}
	}
}
