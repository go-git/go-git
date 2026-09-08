package http

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzSetEscapedPath drives the path re-encoding a redirect target goes
// through. applyRedirect cuts the escaped spelling of a server-chosen path and
// hands the remainder here, so every byte this sees is a byte a redirecting
// server picked.
//
// Three properties are asserted, and none of them restates the function:
//
//   - A malformed escape is refused rather than silently re-escaped. Refusing
//     is what keeps a path the caller never named out of the pack POST, the
//     dumb protocol's object GETs and the retry.
//   - Path and RawPath stay in correspondence. A url.URL whose halves disagree
//     renders a third path through EscapedPath, which is the defect the
//     function exists to prevent.
//   - A spelling a URL can carry survives exactly. Expressed by asking
//     url.Parse for the same path and only requiring the match when the parse
//     round-trips, so the property holds for the inputs a caller can produce
//     without asserting anything about strings no URL can hold.
func FuzzSetEscapedPath(f *testing.F) {
	for _, seed := range []string{
		"",
		"/",
		"/repo.git",
		"/a%2Fb.git",       // an encoded separator: the reason RawPath is kept
		"/a%2fb.git",       // the same separator, lowercased
		"/a%20b.git",       // escapable, but re-escaped to the same spelling
		"/a b.git",         // a byte no EscapedPath renders unescaped
		"/a%zzb.git",       // a malformed escape
		"/a%2",             // a truncated escape
		"/%2e%2e/repo.git", // dot segments spelled so cleaning cannot see them
		"/repo.git%00",
		"/%ff%fe",
		"repo.git", // no leading separator
		"//repo.git",
		"/repo.git?x=1", // a query, which is not part of a path
		"/repo.git#f",
		"/%",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, escaped string) {
		u := &url.URL{Scheme: "https", Host: "example.com", Path: "/stale", RawPath: "/sta%6Cle"}
		err := setEscapedPath(u, escaped)

		decoded, unescapeErr := url.PathUnescape(escaped)
		if unescapeErr != nil {
			require.Error(t, err, "a malformed escape must be refused, not re-escaped")
			return
		}
		require.NoError(t, err)

		require.Equal(t, decoded, u.Path, "Path must be the decoded spelling")
		if u.RawPath != "" {
			require.Equal(t, escaped, u.RawPath,
				"RawPath must be the spelling asked for, or nothing at all")
		}

		// EscapedPath falls back to escaping Path when RawPath is not a valid
		// encoding of it, so whatever it renders must still decode to Path.
		// A URL whose two halves name different paths is one that sends a
		// request somewhere neither the caller nor the redirect chose.
		rendered := u.EscapedPath()
		back, err := url.PathUnescape(rendered)
		require.NoError(t, err, "EscapedPath rendered something it cannot decode: %q", rendered)
		require.Equal(t, u.Path, back, "Path and RawPath disagree: %q vs %q", u.Path, rendered)

		// The exact spelling survives whenever it is one a URL can carry.
		// An EscapedPath of a parsed URL is where every real caller's input
		// comes from, so this covers them all.
		if parsed, perr := url.Parse("https://example.com" + escaped); perr == nil && parsed.EscapedPath() == escaped {
			require.Equal(t, escaped, rendered,
				"the spelling the caller set must be the one the URL renders")
		}
	})
}

// FuzzRedactedQuery drives the query redactor with the bytes it is there for:
// a query reaches it from the repository URL the caller wrote and from the
// Location header a server chose, and whatever it returns is printed into
// every error string and trace line this package emits.
//
// The security property is that no value go-git did not write itself is ever
// rendered. It is stated over the output alone rather than by walking the same
// branches the implementation walks, so the assertion does not agree with a
// mistake in the loop. Two usefulness properties come with it — the element
// count and the parameter names survive — because a redactor that answers
// "REDACTED" to everything satisfies the security property and tells a caller
// nothing about what was sent.
func FuzzRedactedQuery(f *testing.F) {
	for _, seed := range []string{
		"",
		"service=git-upload-pack",
		"service=git-receive-pack",
		"service=glpat-secret", // the allowlisted name, a value go-git did not write
		"service=git-upload-pack;private_token=x",  // the legacy separator net/url does not split on
		"private_token=glpat-secret",               //
		"job_token=secret&service=git-upload-pack", //
		"glpat-secret",                           // a bare value: the name is the secret
		"glpat-secret=",                          // and the same with an empty value
		"=glpat-secret",                          // an empty name
		"a=1&&b=2",                               // an empty element
		"&",                                      //
		"service",                                // the allowlisted name, bare
		"service=git-upload-pack&service=secret", // repeated, one written and one not
		"a=b=c",                                  // a value containing the separator
		"private_token=%67%6c%70%61%74",          // an escaped value
		"REDACTED=REDACTED",                      // the redactor's own output
		"service=git-upload-pack&job_token=REDACTED", //
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		got := redactedQuery(raw)

		if raw == "" {
			require.Empty(t, got, "an empty query stays empty")
			return
		}

		in := strings.Split(raw, "&")
		out := strings.Split(got, "&")
		require.Len(t, out, len(in),
			"an element was added or lost: %q became %q", raw, got)

		for i, elem := range out {
			// An element replaced whole is the case where the name itself
			// could be the secret.
			if elem == "REDACTED" {
				continue
			}

			name, value, hasValue := strings.Cut(elem, "=")
			require.True(t, hasValue,
				"element %d survived unreplaced with no value: %q", i, elem)
			require.True(t, value == "REDACTED" || ownQueryParam(name, value, hasValue),
				"element %d renders a value go-git did not write: %q", i, elem)

			inName, _, _ := strings.Cut(in[i], "=")
			require.Equal(t, inName, name,
				"element %d lost the name that says what was sent", i)
		}

		require.Equal(t, got, redactedQuery(got),
			"redacting twice must not change what a message says")
	})
}
