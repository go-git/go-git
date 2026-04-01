package plugin

import "io"

const objectSignerPlugin Name = "object-signer"

var objectSigner = newKey[Signer](objectSignerPlugin)

// Signer signs arbitrary data and returns the detached signature bytes.
type Signer interface {
	Sign(message io.Reader) ([]byte, error)
}

// ObjectSigner returns the key used to register an object-signing plugin.
// When set, this plugin will set the default signer for new commits and
// tags.
func ObjectSigner() key[Signer] { //nolint:revive // intentional unexported return type
	return objectSigner
}
