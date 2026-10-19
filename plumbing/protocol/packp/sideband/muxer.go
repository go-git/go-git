package sideband

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// Muxer multiplex the packfile along with the progress messages and the error
// information. The multiplex is perform using pktline format.
type Muxer struct {
	max int
	w   io.Writer
}

const chLen = 1

// NewMuxer returns a new Muxer for the given t that writes on w.
//
// If t is equal to `Sideband` the max pack size is set to MaxPackedSize, in any
// other value is given, max pack is set to MaxPackedSize64k, that is the
// maximum length of a line in pktline format.
func NewMuxer(t Type, w io.Writer) *Muxer {
	maxSize := MaxPackedSize64k
	if t == Sideband {
		maxSize = MaxPackedSize
	}

	// Each pkt-line carries a 4-byte length prefix and a 1-byte sideband
	// channel identifier. Reserve space for both so the total packet never
	// exceeds the sideband max size and stays within pktline.MaxPayloadSize.
	return &Muxer{
		max: maxSize - pktline.LenSize - chLen,
		w:   w,
	}
}

// Write writes p in the PackData channel
func (m *Muxer) Write(p []byte) (int, error) {
	return m.WriteChannel(PackData, p)
}

// WriteChannel writes p in the given channel. This method can be used with any
// channel, but is recommend use it only for the ProgressMessage and
// ErrorMessage channels and use Write for the PackData channel
func (m *Muxer) WriteChannel(t Channel, p []byte) (int, error) {
	wrote := 0
	size := len(p)
	for wrote < size {
		n, err := m.doWrite(t, p[wrote:])
		wrote += n

		if err != nil {
			return wrote, err
		}
	}

	return wrote, nil
}

func (m *Muxer) doWrite(ch Channel, p []byte) (int, error) {
	sz := min(len(p), m.max)

	n, err := pktline.Write(m.w, ch.WithPayload(p[:sz]))
	if err != nil {
		// n counts what reached w, which leads with the length prefix and the
		// channel byte. Neither comes from p, so discount both: a write that
		// failed the length check or died on the prefix consumed nothing at
		// all, and claiming otherwise reports bytes the caller still owns.
		return max(0, n-pktline.LenSize-chLen), err
	}

	return sz, nil
}
