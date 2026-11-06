package packp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

func TestDecodeListV2Values(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		line   string
		values []string
	}{
		{line: "key", values: nil},
		{line: "key=", values: []string{""}},
		{line: "key=a", values: []string{"a"}},
		// Values are kept whole, as git hands them to the capability.
		{line: "key=a b", values: []string{"a b"}},
		{line: "key= a", values: []string{" a"}},
		{line: "key=a ", values: []string{"a "}},
		{line: "key=a=b", values: []string{"a=b"}},
	} {
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			_, err := pktline.WriteString(&buf, tc.line+"\n")
			require.NoError(t, err)
			require.NoError(t, pktline.WriteFlush(&buf))

			var l capability.List
			n, err := DecodeListV2(&buf, &l)
			require.NoError(t, err)
			assert.Equal(t, pktline.Flush, n)
			assert.True(t, l.Supports("key"))
			assert.Equal(t, tc.values, l.Get("key"))
		})
	}
}
