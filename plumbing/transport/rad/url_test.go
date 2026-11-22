package rad

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

func TestParseURL_RIDOnly(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("rad://z2cK19PnX6cAUgnZfMfwECBNppJ6z")
	require.NoError(t, err)

	ru, err := parseURL(u)
	require.NoError(t, err)
	assert.Equal(t, "z2cK19PnX6cAUgnZfMfwECBNppJ6z", ru.RID)
	assert.Empty(t, ru.NID)
}

func TestParseURL_RIDAndNID(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("rad://z2cK19PnX6cAUgnZfMfwECBNppJ6z/z6Mkmywxg7Z7e63rkLTx7UJZeH69DLZ9KvhdQEPJ6N9nwVde")
	require.NoError(t, err)

	ru, err := parseURL(u)
	require.NoError(t, err)
	assert.Equal(t, "z2cK19PnX6cAUgnZfMfwECBNppJ6z", ru.RID)
	assert.Equal(t, "z6Mkmywxg7Z7e63rkLTx7UJZeH69DLZ9KvhdQEPJ6N9nwVde", ru.NID)
}

func TestParseURL_CasePreserved(t *testing.T) {
	t.Parallel()

	// Base58 identifiers are case-sensitive; a mixed-case RID must round-trip
	// with its case intact so it maps to the right storage directory.
	const mixed = "z2cK19PnX6cAUgnZfMfwECBNppJ6z"

	u, err := url.Parse("rad://" + mixed)
	require.NoError(t, err)
	require.Equal(t, mixed, u.Host, "url.Parse should preserve Host case")

	ru, err := parseURL(u)
	require.NoError(t, err)
	assert.Equal(t, mixed, ru.RID)
}

func TestParseURL_RejectsThirdSegment(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("rad://a/b/c")
	require.NoError(t, err)

	_, err = parseURL(u)
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrInvalidRequest)
}

func TestParseURL_RejectsEmptyHost(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("rad://")
	require.NoError(t, err)

	_, err = parseURL(u)
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrInvalidRequest)
}

func TestParseURL_RejectsTraversal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		u    *url.URL
	}{
		{"traversal host", &url.URL{Host: "..", Path: ""}},
		{"traversal nid", &url.URL{Host: "z2cK19PnX6cAUgnZfMfwECBNppJ6z", Path: "/.."}},
		{"path separator in host-equivalent segment", &url.URL{Host: "z2cK19PnX6cAUgnZfMfwECBNppJ6z", Path: "/a/b"}},
		{"slash-decoded traversal in nid", &url.URL{Host: "z2cK19PnX6cAUgnZfMfwECBNppJ6z", Path: "/../.."}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseURL(tt.u)
			require.Error(t, err)
			assert.ErrorIs(t, err, transport.ErrInvalidRequest)
		})
	}
}

func TestParseURL_RejectsInvalidCharset(t *testing.T) {
	t.Parallel()

	u := &url.URL{Host: "not/a-valid_rid"}

	_, err := parseURL(u)
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrInvalidRequest)
}

func TestParseURL_RejectsFormsRadicleRejects(t *testing.T) {
	t.Parallel()

	// Every form here is rejected by the real git-remote-rad helper, which
	// splits everything after "rad://" on "/" and accepts only one or two
	// non-empty components. Accepting them would let this transport resolve
	// URLs that Radicle itself refuses.
	const (
		rid = "z2cK19PnX6cAUgnZfMfwECBNppJ6z"
		nid = "z6MktoAvnp6XUueF169dr4quTKnFU4v8e8sz3FMLNnpt53Wg"
	)

	tests := map[string]string{
		"userinfo":               "rad://user@" + rid,
		"userinfo/:pass":         "rad://user:pass@" + rid,
		"query":                  "rad://" + rid + "?x=1",
		"fragment":               "rad://" + rid + "#f",
		"trailing slash":         "rad://" + rid + "/",
		"empty segment":          "rad://" + rid + "//" + nid,
		"trailing empty segment": "rad://" + rid + "/" + nid + "/",
		"opaque, no authority":   "rad:" + rid,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(raw)
			require.NoError(t, err)

			_, err = parseURL(u)
			require.Errorf(t, err, "%q should be rejected", raw)
			assert.ErrorIs(t, err, transport.ErrInvalidRequest)
		})
	}
}
