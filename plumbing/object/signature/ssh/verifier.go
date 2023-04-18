package ssh

import (
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object/signature"
)

// Verifier is an SSH verifier. It can verify an SSH signature using an
// ssh.PublicKey.
//
// TODO(hidde): the PGP counterpart is capable of doing "lists", mostly because
//
//	it can do "keyrings". This is not possible with SSH, so we need to figure out
//	if we want to support multiple keys in the same verifier. The alternative
//	would potentially be a wrapper which can parse an allowed_signers file into
//	a list of verifiers.
type Verifier struct {
	publicKey ssh.PublicKey
}

// Verify verifies an SSH signature using the verifier's public SSH key.
// It returns the signature.Entity that signed the object, or an error.
func (v *Verifier) Verify(o signature.VerifiableObject) (signature.Entity, error) {
	sig, err := sshsig.Unarmor([]byte(o.Signature()))
	if err != nil {
		return nil, err
	}

	encoded := &plumbing.MemoryObject{}
	if err := o.EncodeWithoutSignature(encoded); err != nil {
		return nil, err
	}

	er, err := encoded.Reader()
	if err != nil {
		return nil, err
	}

	if err := sshsig.Verify(er, sig, v.publicKey, sig.HashAlgorithm, sig.Namespace); err != nil {
		return nil, err
	}
	return &Entity{publicKey: v.publicKey}, nil
}
