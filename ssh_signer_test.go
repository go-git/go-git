package git

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSSHSigner_Sign(t *testing.T) {
	t.Parallel()

	// Generate a test key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshSigner, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("failed to create SSH signer: %v", err)
	}

	signer := NewSSHSigner(sshSigner)

	message := []byte("test message to sign")
	signature, err := signer.Sign(bytes.NewReader(message))
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	// Verify the signature format
	if !bytes.HasPrefix(signature, []byte(sshSigArmorHead)) {
		t.Error("signature should start with SSH armor header")
	}
	if !bytes.HasSuffix(bytes.TrimSpace(signature), []byte(sshSigArmorTail)) {
		t.Error("signature should end with SSH armor tail")
	}

	// Verify the signature is valid using SSHVerifier
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	verifier := NewSSHVerifier(map[string]ssh.PublicKey{
		"test": sshPubKey,
	})

	result, err := verifier.Verify(signature, message)
	if err != nil {
		t.Fatalf("verification error: %v", err)
	}

	if !result.Valid {
		t.Errorf("signature should be valid, got error: %v", result.Error)
	}
	if result.Signer != "test" {
		t.Errorf("expected signer 'test', got %q", result.Signer)
	}
}

func TestSSHSigner_SignatureFormat(t *testing.T) {
	t.Parallel()

	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshSigner, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("failed to create SSH signer: %v", err)
	}

	signer := NewSSHSigner(sshSigner)

	signature, err := signer.Sign(bytes.NewReader([]byte("test")))
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	// Check armor format
	sigStr := string(signature)
	if !strings.HasPrefix(sigStr, sshSigArmorHead+"\n") {
		t.Error("signature should have armor header followed by newline")
	}

	// Parse it back to verify structure
	parsed, err := parseSSHSignature(signature)
	if err != nil {
		t.Fatalf("failed to parse generated signature: %v", err)
	}

	if parsed.Version != sshSigVersion {
		t.Errorf("expected version %d, got %d", sshSigVersion, parsed.Version)
	}
	if parsed.Namespace != sshGitNamespace {
		t.Errorf("expected namespace %q, got %q", sshGitNamespace, parsed.Namespace)
	}
	if parsed.HashAlgorithm != "sha512" {
		t.Errorf("expected hash algorithm sha512, got %q", parsed.HashAlgorithm)
	}
}

func TestArmorSSHSignature(t *testing.T) {
	t.Parallel()

	// Test with small data
	data := []byte("test data")
	armored := armorSSHSignature(data)

	if !bytes.HasPrefix(armored, []byte(sshSigArmorHead)) {
		t.Error("missing armor header")
	}
	if !bytes.HasSuffix(bytes.TrimSpace(armored), []byte(sshSigArmorTail)) {
		t.Error("missing armor tail")
	}

	// Test with data that requires wrapping (> 76 chars base64)
	longData := make([]byte, 100)
	for i := range longData {
		longData[i] = byte(i)
	}
	armored = armorSSHSignature(longData)

	// Each line should be at most 76 chars (plus newline)
	lines := strings.Split(string(armored), "\n")
	for i, line := range lines {
		if i == 0 || i == len(lines)-1 || line == "" {
			continue // skip header, tail, and empty lines
		}
		if len(line) > 76 {
			t.Errorf("line %d is too long: %d chars", i, len(line))
		}
	}
}
