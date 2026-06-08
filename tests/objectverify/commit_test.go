//go:build linux

package objectverify_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6"
)

func TestCommitVerifyAlignment(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping commit verify")
	}

	if gpg == nil {
		t.Error("gpg not available")
		t.FailNow()
	}

	cases := []struct {
		name string
		// build returns the unsigned commit bytes that will be signed
		// by gpg. The signature is injected as a single canonical
		// "gpgsig" header in the standard location and the result is
		// what both verifiers see, unless postSign rewrites it.
		build func() []byte
		// postSign optionally rewrites the signed object bytes (e.g.
		// to inject a duplicated signature header). nil means use the
		// standard injection path.
		postSign func(signed []byte, sig string) []byte
	}{
		{
			name: "valid",
			build: func() []byte {
				h, m := canonicalCommit()
				return assembleCommit(h, m)
			},
		},
		{
			name: "duplicate-tree",
			build: func() []byte {
				h, m := canonicalCommit()
				dup := append([]string{
					h[0],
					"tree 5555555555555555555555555555555555555555",
				}, h[1:]...)
				return assembleCommit(dup, m)
			},
		},
		{
			name: "duplicate-author",
			build: func() []byte {
				h, m := canonicalCommit()
				dup := []string{
					h[0],
					h[1],
					h[2],
					"author Override Author <override@example.local> 1700000001 +0000",
					h[3],
				}
				return assembleCommit(dup, m)
			},
		},
		{
			name: "duplicate-committer",
			build: func() []byte {
				h, m := canonicalCommit()
				dup := []string{
					h[0], h[1], h[2], h[3],
					"committer Override Committer <override@example.local> 1700000001 +0000",
				}
				return assembleCommit(dup, m)
			},
		},
		{
			name: "misplaced-parent-after-committer",
			build: func() []byte {
				h, m := canonicalCommit()
				dup := []string{
					h[0], h[1], h[2], h[3],
					"parent 2222222222222222222222222222222222222222",
				}
				return assembleCommit(dup, m)
			},
		},
		{
			// Inject the same signature again as a second gpgsig
			// header. Both occurrences sit before the blank line; both
			// signers strip them when computing the payload.
			name: "double-signature",
			build: func() []byte {
				h, m := canonicalCommit()
				return assembleCommit(h, m)
			},
			postSign: injectGpgSig,
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
			signed := injectGpgSig(unsigned, sig)
			if tc.postSign != nil {
				signed = tc.postSign(signed, sig)
			}

			hash := writeLooseObject(t, repo, "commit", signed)
			gitErr := gitVerifyCommit(t, repo, hash)

			r, err := git.PlainOpen(repo)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()
			commit, err := r.CommitObject(hash)
			ggDecodeErr := err
			var ggVerifyErr error
			if ggDecodeErr == nil {
				_, ggVerifyErr = commit.Verify(gpg.pubKey)
			}

			assertSameVerdict(t, "git verify-commit", gitErr, "go-git Verify",
				combineErr(ggDecodeErr, ggVerifyErr))
		})
	}
}

// canonicalCommit returns the byte-exact unsigned commit body produced by
// upstream `git commit-tree` for a single-parent commit. Used as the
// baseline; individual scenarios mutate the header lines around it.
func canonicalCommit() (headers []string, message string) {
	return []string{
			"tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904",
			"parent 1111111111111111111111111111111111111111",
			"author Test Author <author@example.local> 1700000000 +0000",
			"committer Test Committer <committer@example.local> 1700000000 +0000",
		},
		"signed commit message\n"
}

func assembleCommit(headers []string, message string) []byte {
	var buf bytes.Buffer
	for _, h := range headers {
		buf.WriteString(h)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	buf.WriteString(message)
	return buf.Bytes()
}

// combineErr returns the first non-nil of decode, verify.
// Decode failure is treated the same way as a verify failure.
func combineErr(decode, verify error) error {
	if decode != nil {
		return decode
	}
	return verify
}

// assertSameVerdict fails the test unless both verifiers agree on
// success-or-failure. It does not compare error messages, only verdicts.
func assertSameVerdict(t *testing.T, leftLabel string, leftErr error, rightLabel string, rightErr error) {
	t.Helper()
	leftOK := leftErr == nil
	rightOK := rightErr == nil
	if leftOK == rightOK {
		if !leftOK {
			t.Logf("both verifiers rejected (expected for malformed input)\n  %s: %v\n  %s: %v",
				leftLabel, leftErr, rightLabel, rightErr)
		}
		return
	}
	assert.Failf(t, "verifier verdicts diverge",
		"%s succeeded=%v err=%v\n%s succeeded=%v err=%v",
		leftLabel, leftOK, leftErr, rightLabel, rightOK, rightErr)
}
