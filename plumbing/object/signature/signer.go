package signature

import "github.com/go-git/go-git/v5/plumbing"

// SignableObject is an object which can be signed.
type SignableObject interface {
	Encode(o plumbing.EncodedObject) error
}

// ObjectSigner is capable of signing a SignableObject.
type ObjectSigner interface {
	// Sign signs a SignableObject object. It returns the signature of the
	// object.
	Sign(o SignableObject) (string, error)
}
