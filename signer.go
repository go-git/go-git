package git

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
)

// signableObject is an object which can be signed.
type signableObject interface {
	EncodeWithoutSignature(o plumbing.EncodedObject) error
}

// Signer is an interface for signing git objects.
// message is a reader containing the encoded object to be signed.
// ctx cancels signers that perform external or remote work; purely local
// signers may ignore it.
// Implementors should return the encoded signature and an error if any.
// See https://git-scm.com/docs/gitformat-signature for more information.
type Signer interface {
	Sign(ctx context.Context, message io.Reader) ([]byte, error)
}

func signObject(ctx context.Context, signer Signer, obj signableObject) ([]byte, error) {
	encoded := &plumbing.MemoryObject{}
	if err := obj.EncodeWithoutSignature(encoded); err != nil {
		return nil, err
	}
	r, err := encoded.Reader()
	if err != nil {
		return nil, err
	}

	return signer.Sign(ctx, r)
}
