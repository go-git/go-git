package git

import (
	"io"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Test public key from commit_test.go
const testArmoredPublicKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----

mDMEYGeSihYJKwYBBAHaRw8BAQdAIs9A3YD/EghhAOkHDkxlUkpqYrXUXebLfmmX
+pdEK6C0D2dvLWdpdCB0ZXN0IGtleYiPBBMWCgA3FiEEzKlNMnEN3+oNzzKFjJpp
heC7lfEFAmBnkooCGyMECwkIBwUVCgkICwUWAwIBAAIeAQIXgAAKCRCMmmmF4LuV
8a3jAQCi4hSqjj6J3ch290FvQaYPGwR+EMQTMBG54t+NN6sDfgD/aZy41+0dnFKl
qM/wLW5Wr9XvwH+1zXXbuSvfxasHowq4OARgZ5KKEgorBgEEAZdVAQUBAQdAXoQz
VTYug16SisAoSrxFnOmxmFu6efYgCAwXu0ZuvzsDAQgHiHgEGBYKACAWIQTMqU0y
cQ3f6g3PMoWMmmmF4LuV8QUCYGeSigIbDAAKCRCMmmmF4LuV8Q4QAQCKW5FnEdWW
lHYKeByw3JugnlZ0U3V/R20bCwDglst5UQEAtkN2iZkHtkPly9xapsfNqnrt2gTt
YIefGtzXfldDxg4=
=Psht
-----END PGP PUBLIC KEY BLOCK-----`

// Test signature from commit_test.go (valid for a specific message)
const testValidSignature = `-----BEGIN PGP SIGNATURE-----

iHUEABYKAB0WIQTMqU0ycQ3f6g3PMoWMmmmF4LuV8QUCYGebVwAKCRCMmmmF4LuV
8VtyAP9LbuXAhtK6FQqOjKybBwlV70rLcXVP24ubDuz88VVwSgD+LuObsasWq6/U
TssDKHUR2taa53bQYjkZQBpvvwOrLgc=
=YQUf
-----END PGP SIGNATURE-----`

// getTestSignedMessage generates the exact message that was signed for the test commit.
// This uses the same commit data from commit_test.go TestVerify.
func getTestSignedMessage(t *testing.T) []byte {
	t.Helper()

	ts := time.Unix(1617402711, 0)
	loc, _ := time.LoadLocation("UTC")
	commit := &object.Commit{
		Hash:      plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Author:    object.Signature{Name: "go-git", Email: "go-git@example.com", When: ts.In(loc)},
		Committer: object.Signature{Name: "go-git", Email: "go-git@example.com", When: ts.In(loc)},
		Message:   "test\n",
		TreeHash:  plumbing.NewHash("52a266a58f2c028ad7de4dfd3a72fdf76b0d4e24"),
		ParentHashes: []plumbing.Hash{
			plumbing.NewHash("e4fbb611cd14149c7a78e9c08425f59f4b736a9a"),
		},
	}

	encoded := &plumbing.MemoryObject{}
	if err := commit.EncodeWithoutSignature(encoded); err != nil {
		t.Fatalf("failed to encode commit: %v", err)
	}
	er, err := encoded.Reader()
	if err != nil {
		t.Fatalf("failed to get reader: %v", err)
	}
	message, err := io.ReadAll(er)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	return message
}

func TestOpenPGPVerifier_SupportsSignatureType(t *testing.T) {
	t.Parallel()

	v, err := NewOpenPGPVerifier(testArmoredPublicKey)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

	if !v.SupportsSignatureType(object.SignatureTypeOpenPGP) {
		t.Error("should support OpenPGP")
	}
	if v.SupportsSignatureType(object.SignatureTypeSSH) {
		t.Error("should not support SSH")
	}
	if v.SupportsSignatureType(object.SignatureTypeX509) {
		t.Error("should not support X509")
	}
}

func TestOpenPGPVerifier_ValidSignature(t *testing.T) {
	t.Parallel()

	v, err := NewOpenPGPVerifier(testArmoredPublicKey)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

	testSignedMessage := getTestSignedMessage(t)
	result, err := v.Verify([]byte(testValidSignature), testSignedMessage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Valid {
		t.Errorf("expected valid signature, got invalid: %v", result.Error)
	}
	if result.Type != object.SignatureTypeOpenPGP {
		t.Errorf("expected OpenPGP type, got %v", result.Type)
	}
	if result.TrustLevel != object.TrustFull {
		t.Errorf("expected TrustFull, got %v", result.TrustLevel)
	}
	if result.Signer != "go-git test key" {
		t.Errorf("expected signer 'go-git test key', got %q", result.Signer)
	}
	if result.KeyID == "" {
		t.Error("expected non-empty KeyID")
	}
	if result.PrimaryKeyFingerprint == "" {
		t.Error("expected non-empty PrimaryKeyFingerprint")
	}
}

func TestOpenPGPVerifier_InvalidSignature(t *testing.T) {
	t.Parallel()

	v, err := NewOpenPGPVerifier(testArmoredPublicKey)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

	// Use a tampered message
	tamperedMessage := []byte("tampered message content")
	result, err := v.Verify([]byte(testValidSignature), tamperedMessage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid signature for tampered message")
	}
	if result.Error == nil {
		t.Error("expected error in result for invalid signature")
	}
}

func TestOpenPGPVerifier_MalformedSignature(t *testing.T) {
	t.Parallel()

	v, err := NewOpenPGPVerifier(testArmoredPublicKey)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

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

func TestNewOpenPGPVerifier_EmptyKeyring(t *testing.T) {
	t.Parallel()

	_, err := NewOpenPGPVerifier("")
	if err == nil {
		t.Error("expected error for empty keyring")
	}
}

func TestNewOpenPGPVerifier_InvalidKeyring(t *testing.T) {
	t.Parallel()

	_, err := NewOpenPGPVerifier("not a valid keyring")
	if err == nil {
		t.Error("expected error for invalid keyring")
	}
}
