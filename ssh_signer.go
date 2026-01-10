package git

import (
	"bytes"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"io"

	"golang.org/x/crypto/ssh"
)

// SSHSigner signs git objects using an SSH key.
type SSHSigner struct {
	// Signer is the SSH signer (from ssh.NewSignerFromKey).
	signer ssh.Signer
	// namespace is the signature namespace (should be "git" for commits/tags).
	namespace string
}

// NewSSHSigner creates an SSHSigner with the given SSH signer.
// The namespace defaults to "git" which is correct for commits and tags.
func NewSSHSigner(signer ssh.Signer) *SSHSigner {
	return &SSHSigner{
		signer:    signer,
		namespace: sshGitNamespace,
	}
}

// Sign signs the message using SSH.
func (s *SSHSigner) Sign(message io.Reader) ([]byte, error) {
	data, err := io.ReadAll(message)
	if err != nil {
		return nil, err
	}

	// Hash the message with SHA-512
	h := sha512.New()
	h.Write(data)
	msgHash := h.Sum(nil)

	// Build the signed data structure
	signedData := buildSSHSignedData(s.namespace, "sha512", msgHash)

	// Sign it
	sig, err := s.signer.Sign(rand.Reader, signedData)
	if err != nil {
		return nil, err
	}

	// Build the full signature blob
	sigBlob := buildSSHSignatureBlob(s.signer.PublicKey(), s.namespace, "sha512", sig)

	// Armor it
	return armorSSHSignature(sigBlob), nil
}

func buildSSHSignedData(namespace, hashAlg string, msgHash []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(sshSigMagic)
	writeSSHString(&buf, []byte(namespace))
	writeSSHString(&buf, []byte{}) // reserved
	writeSSHString(&buf, []byte(hashAlg))
	writeSSHString(&buf, msgHash)
	return buf.Bytes()
}

func buildSSHSignatureBlob(pubKey ssh.PublicKey, namespace, hashAlg string, sig *ssh.Signature) []byte {
	var buf bytes.Buffer

	// Magic
	buf.WriteString(sshSigMagic)

	// Version
	binary.Write(&buf, binary.BigEndian, uint32(sshSigVersion))

	// Public key
	writeSSHString(&buf, pubKey.Marshal())

	// Namespace
	writeSSHString(&buf, []byte(namespace))

	// Reserved
	writeSSHString(&buf, []byte{})

	// Hash algorithm
	writeSSHString(&buf, []byte(hashAlg))

	// Signature (format + blob)
	sigBytes := marshalSSHSignature(sig)
	writeSSHString(&buf, sigBytes)

	return buf.Bytes()
}

func marshalSSHSignature(sig *ssh.Signature) []byte {
	var buf bytes.Buffer
	writeSSHString(&buf, []byte(sig.Format))
	writeSSHString(&buf, sig.Blob)
	if len(sig.Rest) > 0 {
		buf.Write(sig.Rest)
	}
	return buf.Bytes()
}

func armorSSHSignature(data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(data)

	// Wrap at 76 characters (standard PEM line length)
	var wrapped bytes.Buffer
	wrapped.WriteString(sshSigArmorHead)
	wrapped.WriteByte('\n')

	for i := 0; i < len(encoded); i += 76 {
		end := min(i+76, len(encoded))
		wrapped.WriteString(encoded[i:end])
		wrapped.WriteByte('\n')
	}

	wrapped.WriteString(sshSigArmorTail)

	return wrapped.Bytes()
}
