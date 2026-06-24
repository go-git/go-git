package capability

import (
	"testing"
)

func FuzzListDecode(f *testing.F) {
	f.Add([]byte("multi_ack"))
	f.Add([]byte("multi_ack thin-pack"))
	f.Add([]byte("agent=git/2.0"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		var l List
		DecodeList(data, &l)
	})
}
