package pgp

import (
	"io"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object/signature"
)

// Verifier is a PGP verifier. It can verify a PGP signature using a list of
// entities.
type Verifier struct {
	entities openpgp.EntityList
}

// NewVerifier creates a new PGP verifier from a list of entities.
func NewVerifier(entities openpgp.EntityList) *Verifier {
	return &Verifier{entities: entities}
}

// NewVerifierFromArmoredKeyRing creates a new PGP verifier from an armored key
// ring. It returns an error if the key ring is not valid.
func NewVerifierFromArmoredKeyRing(r io.Reader) (*Verifier, error) {
	entities, err := openpgp.ReadArmoredKeyRing(r)
	if err != nil {
		return nil, err
	}
	return NewVerifier(entities), nil
}

// Verify verifies a PGP signature using the verifier's entities. It returns the
// signature.Entity that signed the object, or an error.
func (v *Verifier) Verify(o signature.VerifiableObject) (signature.Entity, error) {
	s := strings.NewReader(o.Signature())

	encoded := &plumbing.MemoryObject{}
	if err := o.EncodeWithoutSignature(encoded); err != nil {
		return nil, err
	}

	er, err := encoded.Reader()
	if err != nil {
		return nil, err
	}

	entity, err := openpgp.CheckArmoredDetachedSignature(v.entities, er, s, nil)
	if err != nil {
		return nil, err
	}

	return &Entity{entity: entity}, nil
}
