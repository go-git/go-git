package pgp

import (
	"bytes"
	"errors"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object/signature"
)

// Signer is a PGP signer. It can sign a signature.SignableObject object using
// an openpgp.Entity.
type Signer struct {
	entity *openpgp.Entity
}

// NewSigner returns a new Signer using the given openpgp.Entity.
func NewSigner(entity *openpgp.Entity) (*Signer, error) {
	if entity == nil {
		return nil, errors.New("can not create a signer with a nil entity")
	}
	return &Signer{entity: entity}, nil
}

// Sign signs a signature.SignableObject object using the Signer's
// openpgp.Entity. It returns the signature of the object, or an error.
func (s *Signer) Sign(o signature.SignableObject) (string, error) {
	encoded := &plumbing.MemoryObject{}
	if err := o.Encode(encoded); err != nil {
		return "", err
	}

	r, err := encoded.Reader()
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&b, s.entity, r, nil); err != nil {
		return "", err
	}
	return b.String(), nil
}
