package git

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	sshSigMagic     = "SSHSIG"
	sshSigVersion   = 1
	sshSigArmorHead = "-----BEGIN SSH SIGNATURE-----"
	sshSigArmorTail = "-----END SSH SIGNATURE-----"
)

// sshSignature represents a parsed SSH signature.
type sshSignature struct {
	Version       uint32
	PublicKey     ssh.PublicKey
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Signature     *ssh.Signature
}

// Fingerprint returns the SSH key fingerprint (SHA256:...).
func (s *sshSignature) Fingerprint() string {
	return ssh.FingerprintSHA256(s.PublicKey)
}

// parseSSHSignature parses an armored SSH signature.
func parseSSHSignature(armored []byte) (*sshSignature, error) {
	// Strip armor
	content := string(armored)
	if !strings.HasPrefix(content, sshSigArmorHead) {
		return nil, fmt.Errorf("missing SSH signature header")
	}
	content = strings.TrimPrefix(content, sshSigArmorHead)
	content = strings.TrimSuffix(strings.TrimSpace(content), sshSigArmorTail)
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, "\r", "")

	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	return parseSSHSignatureBlob(data)
}

func parseSSHSignatureBlob(data []byte) (*sshSignature, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("signature too short")
	}

	// Check magic
	if string(data[:6]) != sshSigMagic {
		return nil, fmt.Errorf("invalid magic: expected %q, got %q", sshSigMagic, string(data[:6]))
	}

	r := bytes.NewReader(data[6:])
	sig := &sshSignature{}

	// Read version
	if err := binary.Read(r, binary.BigEndian, &sig.Version); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	if sig.Version != sshSigVersion {
		return nil, fmt.Errorf("unsupported version: %d", sig.Version)
	}

	// Read public key
	pubKeyBytes, err := readSSHString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key: %w", err)
	}
	sig.PublicKey, err = ssh.ParsePublicKey(pubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	// Read namespace
	nsBytes, err := readSSHString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read namespace: %w", err)
	}
	sig.Namespace = string(nsBytes)

	// Read reserved
	reservedBytes, err := readSSHString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read reserved: %w", err)
	}
	sig.Reserved = string(reservedBytes)

	// Read hash algorithm
	hashBytes, err := readSSHString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read hash algorithm: %w", err)
	}
	sig.HashAlgorithm = string(hashBytes)

	// Read signature
	sigBytes, err := readSSHString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read signature: %w", err)
	}
	sig.Signature, err = parseSSHSignatureWireFormat(sigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signature: %w", err)
	}

	return sig, nil
}

// parseSSHSignatureWireFormat parses an SSH signature from its wire format.
// The format is: string (algorithm) + string (signature blob).
func parseSSHSignatureWireFormat(data []byte) (*ssh.Signature, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("signature data too short")
	}

	r := bytes.NewReader(data)

	// Read algorithm/format string
	formatBytes, err := readSSHString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read signature format: %w", err)
	}

	// Read signature blob
	blobBytes, err := readSSHString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read signature blob: %w", err)
	}

	// Collect any remaining bytes
	var rest []byte
	if r.Len() > 0 {
		rest = make([]byte, r.Len())
		r.Read(rest)
	}

	return &ssh.Signature{
		Format: string(formatBytes),
		Blob:   blobBytes,
		Rest:   rest,
	}, nil
}

func readSSHString(r *bytes.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	if length > 1<<20 { // 1MB limit for sanity
		return nil, fmt.Errorf("string too long: %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}
