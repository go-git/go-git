package packp

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/stretchr/testify/assert"
)

// returns a byte slice with the pkt-lines for the given payloads.
func pktlines(t *testing.T, payloads ...string) []byte {
	var buf bytes.Buffer

	comment := fmt.Sprintf("building pktlines for %v\n", payloads)
	for _, p := range payloads {
		if p == "" {
			assert.NoError(t, pktline.WriteFlush(&buf), comment)
		} else {
			_, err := pktline.WriteString(&buf, p)
			assert.NoError(t, err, comment)
		}
	}

	return buf.Bytes()
}

func toPktLines(t *testing.T, payloads []string) io.Reader {
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			assert.Nil(t, pktline.WriteFlush(&buf))
		} else {
			_, err := pktline.WriteString(&buf, p)
			assert.NoError(t, err)
		}
	}

	return &buf
}
