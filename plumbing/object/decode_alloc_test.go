package object

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

// TestDecodeContinuationLinesAllocateLinearly guards against header values
// being rebuilt as immutable strings on every continuation line. That makes
// the bytes copied for n equal-length lines proportional to 1 + 2 + ... + n,
// so a structurally valid object of a few hundred KiB costs hundreds of MiB
// to decode. Signatures need not verify for the object to reach the decoder.
func TestDecodeContinuationLinesAllocateLinearly(t *testing.T) { //nolint:paralleltest // reads process-wide runtime.MemStats
	const (
		lines   = 4000
		tree    = "tree eba74343e2f15d62adedfd8c883ee0262b5c8021\n"
		ident   = "Foo <foo@example.com> 1700000000 +0000"
		object  = "object eba74343e2f15d62adedfd8c883ee0262b5c8021\n"
		maxGrow = 16
	)
	payload := strings.Repeat("A", 64)

	continued := func(header string) (string, int) {
		var b strings.Builder
		b.WriteString(header + " " + payload + "\n")
		for range lines - 1 {
			b.WriteString(" " + payload + "\n")
		}
		return b.String(), lines * (len(payload) + 1)
	}
	fragmented := func(header string) (string, int) {
		var b strings.Builder
		for range lines {
			b.WriteString(header + " " + payload + "\n")
		}
		return b.String(), lines * (len(payload) + 1)
	}
	commitHeaders := tree + "author " + ident + "\ncommitter " + ident + "\n"
	tagHeaders := object + "type commit\ntag v1\ntagger " + ident + "\n"

	type testCase struct {
		name    string
		typ     plumbing.ObjectType
		headers string
		body    func(string) (string, int)
		header  string
		value   func(*Commit, *Tag) string
	}
	cases := []testCase{
		{
			name: "commit gpgsig continuation", typ: plumbing.CommitObject,
			headers: commitHeaders, body: continued, header: headerpgp,
			value: func(c *Commit, _ *Tag) string { return c.Signature },
		},
		{
			name: "commit gpgsig fragmented", typ: plumbing.CommitObject,
			headers: commitHeaders, body: fragmented, header: headerpgp,
			value: func(c *Commit, _ *Tag) string { return c.Signature },
		},
		{
			name: "commit gpgsig-sha256 continuation", typ: plumbing.CommitObject,
			headers: commitHeaders, body: continued, header: headerpgp256,
			value: func(c *Commit, _ *Tag) string { return c.SignatureSHA256 },
		},
		{
			name: "commit gpgsig-sha256 fragmented", typ: plumbing.CommitObject,
			headers: commitHeaders, body: fragmented, header: headerpgp256,
			value: func(c *Commit, _ *Tag) string { return c.SignatureSHA256 },
		},
		{
			name: "commit extra header continuation", typ: plumbing.CommitObject,
			headers: commitHeaders, body: continued, header: "x-extra",
			value: func(c *Commit, _ *Tag) string {
				if len(c.ExtraHeaders) != 1 {
					return ""
				}
				return c.ExtraHeaders[0].Value + "\n"
			},
		},
		{
			name: "tag gpgsig-sha256 continuation", typ: plumbing.TagObject,
			headers: tagHeaders, body: continued, header: headerpgp256,
			value: func(_ *Commit, tg *Tag) string { return tg.SignatureSHA256 },
		},
		{
			name: "tag gpgsig-sha256 fragmented", typ: plumbing.TagObject,
			headers: tagHeaders, body: fragmented, header: headerpgp256,
			value: func(_ *Commit, tg *Tag) string { return tg.SignatureSHA256 },
		},
	}

	for _, tc := range cases { //nolint:paralleltest // reads process-wide runtime.MemStats
		t.Run(tc.name, func(t *testing.T) {
			body, valueLen := tc.body(tc.header)
			raw := tc.headers + body + "\nmsg\n"

			obj := &plumbing.MemoryObject{}
			obj.SetType(tc.typ)
			_, err := obj.Write([]byte(raw))
			require.NoError(t, err)

			var (
				c  Commit
				tg Tag
				ms runtime.MemStats
			)
			runtime.ReadMemStats(&ms)
			before := ms.TotalAlloc
			if tc.typ == plumbing.CommitObject {
				err = c.Decode(obj)
			} else {
				err = tg.Decode(obj)
			}
			runtime.ReadMemStats(&ms)
			allocated := ms.TotalAlloc - before

			require.NoError(t, err)
			assert.Len(t, tc.value(&c, &tg), valueLen)
			assert.LessOrEqualf(t, allocated, uint64(maxGrow*len(raw)),
				"decoding a %d byte object allocated %d bytes", len(raw), allocated)
		})
	}
}
