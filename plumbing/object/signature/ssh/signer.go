package ssh

import (
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object/signature"
)

// namespace is the Git namespace for SSH signatures.
// TODO(hidde): need to confirm how this relates to defining "namespaces" in
//
//	allowed_signers file.
const namespace = "git"

// Signer is an SSH signer. It can sign a signature.SignableObject object using
// an ssh.Signer.
type Signer struct {
	signer    ssh.Signer
	algorithm sshsig.HashAlgorithm
}

// NewSigner returns a new Signer using the given ssh.Signer and hash algorithm.
func NewSigner(signer ssh.Signer, algorithm sshsig.HashAlgorithm) (*Signer, error) {
	return &Signer{signer: signer, algorithm: algorithm}, nil
}

// NewDefaultSigner returns a new Signer using the given ssh.Signer. It uses
// sshsig.HashSHA512 as the default hash algorithm.
func NewDefaultSigner(signer ssh.Signer) (*Signer, error) {
	return NewSigner(signer, sshsig.HashSHA512)
}

// Sign signs a signature.SignableObject object using the Signer's ssh.Signer.
// It returns the signature of the object in armored (PEM) format, or an
// error.
func (s *Signer) Sign(o signature.SignableObject) (string, error) {
	encoded := &plumbing.MemoryObject{}
	if err := o.Encode(encoded); err != nil {
		return "", err
	}

	r, err := encoded.Reader()
	if err != nil {
		return "", err
	}

	sig, err := sshsig.Sign(r, s.signer, s.algorithm, namespace)
	if err != nil {
		return "", err
	}
	return string(sshsig.Armor(sig)), nil
}
