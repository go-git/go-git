package plugin

import "io"

const objectSignerPlugin Name = "object-signer"

var objectSigner = newKey[Signer](objectSignerPlugin)

type Signer interface {
	Sign(message io.Reader) ([]byte, error)
}

// ObjectSigner is key used to represent the plugin for object signing.
// When set, this plugin will set the default signer for new commits and
// tags.
func ObjectSigner() key[Signer] {
	return objectSigner
}
