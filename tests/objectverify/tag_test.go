//go:build linux

package objectverify_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6"
)

func TestTagVerifyAlignment(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping tag verify")
	}

	if gpg == nil {
		t.Error("gpg not available")
		t.FailNow()
	}

	cases := []struct {
		name string
		// build returns the unsigned tag bytes that will be signed by
		// gpg. The signature is appended inline after the body, the
		// canonical layout produced by `git tag -s`.
		build func() []byte
		// postSign optionally rewrites the signed object bytes (e.g.
		// to append a second inline signature block).
		postSign func(t *testing.T, signed []byte) []byte
	}{
		{
			name: "valid",
			build: func() []byte {
				h, m := canonicalTag()
				return assembleTag(h, m)
			},
		},
		{
			name: "duplicate-tag",
			build: func() []byte {
				h, m := canonicalTag()
				dup := []string{
					h[0], h[1], h[2],
					"tag v1-override",
					h[3],
				}
				return assembleTag(dup, m)
			},
		},
		{
			name: "duplicate-tagger",
			build: func() []byte {
				h, m := canonicalTag()
				dup := []string{
					h[0], h[1], h[2], h[3],
					"tagger Override Tagger <override@example.local> 1700000001 +0000",
				}
				return assembleTag(dup, m)
			},
		},
		{
			name: "double-signature",
			build: func() []byte {
				h, m := canonicalTag()
				return assembleTag(h, m)
			},
			postSign: func(t *testing.T, signed []byte) []byte {
				// Append a second inline signature over the
				// already-signed bytes. parse_signature in
				// upstream and parseSignedBytes in go-git both
				// pick the last PGP block, so the signed
				// payload is the original tag plus the first
				// signature.
				sig2 := gpgSign(t, signed)
				return append(append([]byte{}, signed...), []byte(sig2)...)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.name == "double-signature" && os.Getenv("GIT_VERSION") == "v2.11.0" {
				t.Skip("multi-signature rejection not yet established in Git 2.11.0")
			}

			repo := initRepo(t)
			unsigned := tc.build()
			sig := gpgSign(t, unsigned)
			signed := append(append([]byte{}, unsigned...), []byte(sig)...)
			if tc.postSign != nil {
				signed = tc.postSign(t, signed)
			}

			hash := writeLooseObject(t, repo, "tag", signed)
			gitErr := gitVerifyTag(t, repo, hash)

			r, err := git.PlainOpen(repo)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()
			tag, err := r.TagObject(hash)
			ggDecodeErr := err
			var ggVerifyErr error
			if ggDecodeErr == nil {
				_, ggVerifyErr = tag.Verify(gpg.pubKey)
			}

			assertSameVerdict(t, "git verify-tag", gitErr, "go-git Verify",
				combineErr(ggDecodeErr, ggVerifyErr))
		})
	}
}

// canonicalTag returns the byte-exact unsigned annotated-tag body
// produced by upstream `git tag -a` on a commit. Individual scenarios
// mutate the header lines around it.
func canonicalTag() (headers []string, message string) {
	return []string{
			"object 1eca38290a3131d0c90709496a9b2207a872631e",
			"type commit",
			"tag v1",
			"tagger Test Tagger <tagger@example.local> 1700000000 +0000",
		},
		"signed annotated tag\n"
}

func assembleTag(headers []string, message string) []byte {
	var buf bytes.Buffer
	for _, h := range headers {
		buf.WriteString(h)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	buf.WriteString(message)
	return buf.Bytes()
}
