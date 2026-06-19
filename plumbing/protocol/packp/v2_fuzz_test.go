package packp

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func FuzzV2CapabilitiesDecode(f *testing.F) {
	f.Add([]byte("0010version 2\n0000"))
	f.Add([]byte("0010version 2\n0012agent=git/2.40\n0023fetch=shallow filter wait-for-done\n0000"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		var c V2Capabilities
		_ = c.Decode(bytes.NewReader(data))
	})
}

func FuzzV2CapabilitiesDecodeList(f *testing.F) {
	f.Add([]byte("0012agent=git/2.40\n0000"))
	f.Add([]byte("000bls-refs0000"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		var c V2Capabilities
		_ = c.DecodeList(bytes.NewReader(data))
	})
}

func FuzzFetchResponseV2Decode(f *testing.F) {
	f.Add([]byte("0013acknowledgments\n0008NAK\n0000"))
	f.Add([]byte("0013acknowledgments\n0035ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0000"))
	f.Add([]byte("000cshallow-info\n0038shallow 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0000"))
	f.Add([]byte("0009packfile\n"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		var r FetchResponseV2
		_ = r.Decode(bytes.NewReader(data))
	})
}

func FuzzLsRefsResponseDecode(f *testing.F) {
	f.Add([]byte("0032ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\n0000"))
	f.Add([]byte("004e6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD symref-target:refs/heads/main\n0000"))
	f.Add([]byte("0009unborn HEAD\n0000"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		var r LsRefsResponse
		_ = r.Decode(bytes.NewReader(data))
	})
}

func FuzzLsRefsRequestEncode(f *testing.F) {
	f.Add("agent=git/2.40", "refs/heads/", true, true, false)
	f.Add("", "", false, false, false)

	f.Fuzz(func(t *testing.T, capability, refPrefix string, symrefs, peel, unborn bool) {
		req := &LsRefsRequest{
			Capabilities: []string{capability},
			Symrefs:      symrefs,
			Peel:         peel,
			Unborn:       unborn,
			RefPrefixes:  []string{refPrefix},
		}

		var buf bytes.Buffer
		if err := req.Encode(&buf); err != nil {
			return
		}
		assertValidPktLines(t, buf.Bytes())
	})
}

func FuzzFetchRequestV2Encode(f *testing.F) {
	f.Add("agent=git/2.40", "blob:none", 1, true, false)
	f.Add("", "", 0, false, false)

	f.Fuzz(func(t *testing.T, capability, filter string, depth int, done, includeTag bool) {
		req := &FetchRequestV2{
			Capabilities: []string{capability},
			Wants:        []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
			Filter:       Filter(filter),
			Depth:        depth,
			Done:         done,
			IncludeTag:   includeTag,
			OfsDelta:     true,
		}

		var buf bytes.Buffer
		if err := req.Encode(&buf); err != nil {
			return
		}
		assertValidPktLines(t, buf.Bytes())
	})
}

// assertValidPktLines verifies that a successfully-encoded request is a
// well-formed sequence of pkt-lines: every byte is consumed by ReadLine
// without error before EOF.
func assertValidPktLines(t *testing.T, data []byte) {
	t.Helper()
	r := bytes.NewReader(data)
	for {
		_, _, err := pktline.ReadLine(r)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("Encode produced an invalid pkt-line stream: %v\n%q", err, data)
		}
	}
}
