package sign

import (
	"bytes"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	magicPreamble = "SSHSIG"
	sigVersion    = 1
	defaultHash   = "sha512"
	namespace     = "git"
)

// SSHSigner implements the Signer interface using SSH keys
type SSHSigner struct {
	signer ssh.Signer
}

// NewSSHSigner creates a new SSHSigner from a private key file path
func NewSSHSigner(keyPath string) (*SSHSigner, error) {
	if strings.HasPrefix(keyPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		keyPath = filepath.Join(home, keyPath[2:])
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return &SSHSigner{signer: signer}, nil
}

// Sign implements the signing protocol matching OpenSSH's ssh-keygen -Y sign
// ported from openssh/openssh-portable/sshsig.c
func (s *SSHSigner) Sign(r io.Reader) ([]byte, error) {
	// Read and hash the message
	msg, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read message: %v", err)
	}

	// Hash the message with SHA-512 (matching OpenSSH's default)
	hasher := sha512.New()
	hasher.Write(msg)
	hashedMsg := hasher.Sum(nil)

	// Get the public key in SSH wire format
	pubKey := s.signer.PublicKey()
	pubKeyBytes := pubKey.Marshal()

	// Build the data to be signed, matching OpenSSH's sshsig_wrap_sign
	signData := buildSigningBlob(hashedMsg)

	// Sign the blob
	signature, err := s.signer.Sign(rand.Reader, signData)
	if err != nil {
		return nil, fmt.Errorf("failed to sign data: %v", err)
	}

	// Build the final signature blob
	sigBlob := buildSignatureBlob(pubKeyBytes, signature)

	// Armor the signature
	return armorSignature(sigBlob), nil
}

// buildSigningBlob constructs the blob to be signed, matching OpenSSH's format
func buildSigningBlob(hashedMsg []byte) []byte {
	var buf bytes.Buffer

	// Magic identifier
	buf.WriteString(magicPreamble)

	// Namespace (always "git" for git)
	writeString(&buf, []byte(namespace))

	// Reserved (empty string)
	writeString(&buf, []byte(""))

	// Hash algorithm
	writeString(&buf, []byte(defaultHash))

	// Hashed message
	writeString(&buf, hashedMsg)

	return buf.Bytes()
}

// buildSignatureBlob constructs the final signature blob
func buildSignatureBlob(pubKeyBytes []byte, signature *ssh.Signature) []byte {
	var buf bytes.Buffer

	// Magic identifier
	buf.WriteString(magicPreamble)

	// Version
	writeUint32(&buf, sigVersion)

	// Public key
	writeString(&buf, pubKeyBytes)

	// Namespace (always "git" for git)
	writeString(&buf, []byte(namespace))

	// Reserved
	writeString(&buf, []byte(""))

	// Hash algorithm
	writeString(&buf, []byte(defaultHash))

	// SSH signature
	writeString(&buf, ssh.Marshal(signature))

	return buf.Bytes()
}

// writeUint32 writes a 32-bit unsigned integer in big-endian format
func writeUint32(buf *bytes.Buffer, v uint32) {
	binary.Write(buf, binary.BigEndian, v)
}

// writeString writes an SSH string (length prefix + data)
func writeString(buf *bytes.Buffer, data []byte) {
	writeUint32(buf, uint32(len(data)))
	buf.Write(data)
}

// armorSignature adds the SSH signature armor
func armorSignature(data []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("-----BEGIN SSH SIGNATURE-----\n")

	// Base64 encode with 70-character line wrapping
	encoded := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(encoded); i += 70 {
		end := i + 70
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end] + "\n")
	}

	buf.WriteString("-----END SSH SIGNATURE-----\n")
	return buf.Bytes()
}
