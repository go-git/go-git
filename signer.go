package git

import (
	"context"
	"io"
)

// signableObject is an object which can be signed.
type signableObject interface {
	EncodeWithoutSignature() (io.Reader, error)
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

func signObject(signer Signer, obj signableObject) ([]byte, error) {
	message, err := obj.EncodeWithoutSignature()
	if err != nil {
		return nil, err
	}

	// TODO: thread a caller-supplied context once Worktree.Commit accepts one.
	return signer.Sign(context.TODO(), message)
}
