package git

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"hash"
	"maps"

	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/plumbing/object"
)

const sshGitNamespace = "git"

// SSHVerifier verifies SSH signatures.
type SSHVerifier struct {
	// allowedSigners maps principal names (e.g., email) to their public keys.
	// If a key is in this map, signatures from it will have TrustFull.
	// Keys not in the map will have TrustUndefined.
	allowedSigners map[string]ssh.PublicKey
}

// NewSSHVerifier creates an SSH verifier with the given allowed signers.
// The provided map is copied to prevent external modification.
func NewSSHVerifier(allowedSigners map[string]ssh.PublicKey) *SSHVerifier {
	copied := make(map[string]ssh.PublicKey, len(allowedSigners))
	maps.Copy(copied, allowedSigners)
	return &SSHVerifier{allowedSigners: copied}
}

// SupportsSignatureType returns true for SSH signatures.
func (v *SSHVerifier) SupportsSignatureType(t object.SignatureType) bool {
	return t == object.SignatureTypeSSH
}

// Verify checks an SSH signature against the message.
func (v *SSHVerifier) Verify(signature, message []byte) (*object.VerificationResult, error) {
	result := &object.VerificationResult{
		Type: object.SignatureTypeSSH,
	}

	// Parse the SSH signature
	sig, err := parseSSHSignature(signature)
	if err != nil {
		result.Valid = false
		result.Error = fmt.Errorf("failed to parse SSH signature: %w", err)
		return result, nil
	}

	// Check namespace
	if sig.Namespace != sshGitNamespace {
		result.Valid = false
		result.Error = fmt.Errorf("invalid namespace: expected %q, got %q", sshGitNamespace, sig.Namespace)
		return result, nil
	}

	result.KeyID = sig.Fingerprint()

	// Compute the signed data (per PROTOCOL.sshsig)
	signedData, err := computeSSHSignedData(sig.Namespace, sig.HashAlgorithm, message)
	if err != nil {
		result.Valid = false
		result.Error = err
		return result, nil
	}

	// Verify the signature
	if err := sig.PublicKey.Verify(signedData, sig.Signature); err != nil {
		result.Valid = false
		result.Error = fmt.Errorf("signature verification failed: %w", err)
		return result, nil
	}

	result.Valid = true

	// Check if key is in allowed signers
	result.TrustLevel = object.TrustUndefined
	for principal, allowedKey := range v.allowedSigners {
		if sshKeysEqual(sig.PublicKey, allowedKey) {
			result.TrustLevel = object.TrustFull
			result.Signer = principal
			break
		}
	}

	return result, nil
}

// computeSSHSignedData computes the data structure that SSH actually signs.
// Format per PROTOCOL.sshsig: MAGIC_PREAMBLE || namespace || reserved || hash_algorithm || H(message)
func computeSSHSignedData(namespace, hashAlg string, message []byte) ([]byte, error) {
	var h hash.Hash
	switch hashAlg {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return nil, fmt.Errorf("unsupported hash algorithm: %s", hashAlg)
	}

	h.Write(message)
	msgHash := h.Sum(nil)

	// Build the signed data structure
	var buf bytes.Buffer
	buf.WriteString(sshSigMagic)
	writeSSHString(&buf, []byte(namespace))
	writeSSHString(&buf, []byte{}) // reserved
	writeSSHString(&buf, []byte(hashAlg))
	writeSSHString(&buf, msgHash)

	return buf.Bytes(), nil
}

func sshKeysEqual(a, b ssh.PublicKey) bool {
	return bytes.Equal(a.Marshal(), b.Marshal())
}

func writeSSHString(buf *bytes.Buffer, data []byte) {
	length := uint32(len(data))
	// binary.Write to bytes.Buffer never fails for fixed-size types
	_ = binary.Write(buf, binary.BigEndian, length)
	buf.Write(data)
}
