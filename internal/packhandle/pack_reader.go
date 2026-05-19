package packhandle

import "io"

// PackReader provides streaming access to the .pack file.
// Returned by [PackHandle.OpenPackReader]. Each call returns a
// fresh cursor with its own offset.
type PackReader interface {
	io.Reader
	io.Seeker
	io.Closer
}

// RandomReader provides random-access reads against the .pack
// file. Returned by [PackHandle.OpenRandomReader]. ReadAt is
// safe to call concurrently with itself and across cursors.
type RandomReader interface {
	io.ReaderAt
	io.Closer
}
