package plugin

import "io"

const objectSignerPlugin Name = "object-signer"

var objectSigner = newKey[Signer](objectSignerPlugin)

type Signer interface {
	Sign(message io.Reader) ([]byte, error)
}

// ObjectSigner returns the key used to register an object-signing plugin.
// When set, this plugin will set the default signer for new commits and
// tags.
func ObjectSigner() key[Signer] {
	return objectSigner
}
