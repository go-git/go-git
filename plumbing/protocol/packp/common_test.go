package packp

import (
	"bytes"
	"io"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

// returns a byte slice with the pkt-lines for the given payloads.
func pktlines(c *C, payloads ...string) []byte {
	var buf bytes.Buffer

	comment := Commentf("building pktlines for %v\n", payloads)
	for _, p := range payloads {
		if p == "" {
			c.Assert(pktline.WriteFlush(&buf), IsNil, comment)
		} else {
			_, err := pktline.WritePacketString(&buf, p)
			c.Assert(err, IsNil, comment)
		}
	}

	return buf.Bytes()
}

func toPktLines(c *C, payloads []string) io.Reader {
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			c.Assert(pktline.WriteFlush(&buf), IsNil)
		} else {
			_, err := pktline.WritePacketString(&buf, p)
			c.Assert(err, IsNil)
		}
	}

	return &buf
}
