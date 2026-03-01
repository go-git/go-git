package plugin

import "github.com/go-git/go-git/v6/plumbing/object"

const objectVerifierPlugin Name = "object-verifier"

var objectVerifier = newKey[Verifier](objectVerifierPlugin)

// Verifier is an interface for verifying cryptographic signatures on git objects.
// This is defined locally in the plugin package to avoid import cycles with the
// git package.
type Verifier interface {
	Verify(signature, message []byte) (*object.VerificationResult, error)
}

// ObjectVerifier is key used to represent the plugin for object verification.
// When set, this plugin will provide the default verifier for commits and tags
// retrieved through higher-level APIs (e.g. Repository.CommitObject).
func ObjectVerifier() key[Verifier] {
	return objectVerifier
}
