package object

// ObjectOption is a functional option for configuring object construction.
type ObjectOption func(*ObjectOptions)

// ObjectOptions holds configuration for object construction.
type ObjectOptions struct {
	// Verifier is the signature verifier to inject into the object.
	Verifier Verifier
}

// WithVerifier returns an ObjectOption that injects a Verifier into the object.
// The verifier propagates through object navigation (e.g. commit.Parent(),
// tag.Commit()) so that Verify() works on the resulting objects without
// requiring the caller to pass a verifier each time.
func WithVerifier(v Verifier) ObjectOption {
	return func(o *ObjectOptions) {
		o.Verifier = v
	}
}

// applyObjectOptions applies all ObjectOptions and returns the result.
func applyObjectOptions(opts []ObjectOption) ObjectOptions {
	var o ObjectOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
