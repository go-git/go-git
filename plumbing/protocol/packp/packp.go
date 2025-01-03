package packp

import "io"

// Encoder is the interface implemented by an object that can encode itself
// into a [io.Writer].
type Encoder interface {
	Encode(w io.Writer) error
}

// Decoder is the interface implemented by an object that can decode itself
// from a [io.Reader].
type Decoder interface {
	Decode(r io.Reader) error
}
