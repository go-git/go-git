package revfile

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
)

func FuzzDecode(f *testing.F) {
	// Minimal valid rev header: 4-byte magic + version(uint32 BE = 1) +
	// hash-func(uint32 BE = 1, SHA1).
	f.Add([]byte("RIDX\x00\x00\x00\x01\x00\x00\x00\x01"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		out := make(chan uint32, 64)
		// Drain the channel concurrently so Decode never blocks.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for v := range out {
				_ = v
			}
		}()

		var packID plumbing.Hash
		_ = Decode(bytes.NewReader(data), 0, packID, out)
		<-done
	})
}
