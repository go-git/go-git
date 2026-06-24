package packfile

import (
	"bytes"
	"testing"
)

func FuzzDiffDelta(f *testing.F) {
	f.Add([]byte{}, []byte{})
	f.Add([]byte("foo"), []byte("bar"))
	// Identity: forces a single large copy and the maxCopySize loop.
	f.Add(bytes.Repeat([]byte("abc"), 1024), bytes.Repeat([]byte("abc"), 1024))
	// Small src / large tgt: forces inserts on the short-src branch.
	f.Add([]byte("ab"), bytes.Repeat([]byte("x"), 4096))
	// Large src / small tgt: forces copies on the short-remainder branch.
	f.Add(bytes.Repeat([]byte("x"), 4096), []byte("ab"))
	// Repetitive content: stresses deltaIndex collisions.
	f.Add(bytes.Repeat([]byte("AB"), 8192), bytes.Repeat([]byte("BA"), 8192))

	f.Fuzz(func(_ *testing.T, src, tgt []byte) {
		delta := DiffDelta(src, tgt)
		if len(src) == 0 {
			// PatchDelta refuses empty source by contract; the
			// DiffDelta call above still exercises the encoder.
			return
		}
		_, _ = PatchDelta(src, delta)
	})
}
