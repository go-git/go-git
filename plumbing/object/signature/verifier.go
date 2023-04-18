package signature

import (
	"github.com/go-git/go-git/v5/plumbing"
)

// VerifiableObject is an object which signature can be verified.
type VerifiableObject interface {
	// Signature returns the signature of the object.
	Signature() string
	// EncodeWithoutSignature encodes the object without the Signature.
	EncodeWithoutSignature(o plumbing.EncodedObject) error
}

// ObjectVerifier is capable of verifying the signature of a VerifiableObject.
type ObjectVerifier interface {
	// Verify verifies a VerifiableObject object. It returns the Entity that
	// signed the object, or an error if the verification failed.
	Verify(o VerifiableObject) (Entity, error)
}
