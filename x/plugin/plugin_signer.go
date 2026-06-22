package plugin

import (
	"context"
	"io"
)

const objectSignerPlugin Name = "object-signer"

var objectSigner = newKey[Signer](objectSignerPlugin)

// Signer signs arbitrary data and returns the detached signature bytes.
// ctx cancels signers that perform external or remote work; purely local
// signers may ignore it.
type Signer interface {
	Sign(ctx context.Context, message io.Reader) ([]byte, error)
}

// ObjectSigner returns the key used to register an object-signing plugin.
// When set, this plugin will set the default signer for new commits and
// tags.
func ObjectSigner() key[Signer] { //nolint:revive // intentional unexported return type
	return objectSigner
}
