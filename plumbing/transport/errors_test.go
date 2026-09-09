package transport

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// CredentialsDroppedError is exported and its fields are ordinary *url.URL, so
// a caller can build one directly without going through a transport —
// including with a nil From or To. Both are rendered with %s, which prints a
// nil *url.URL as "<nil>"; reaching for String() or Redacted() instead would
// dereference it and panic inside an error's Error, which is the worst place
// for one.
func TestCredentialsDroppedErrorMessage(t *testing.T) {
	t.Parallel()

	to := &url.URL{Scheme: "https", Host: "dest.example"}
	from := &url.URL{Scheme: "https", Host: "origin.example"}

	for _, tc := range []struct {
		name     string
		from, to *url.URL
		want     string
	}{
		{
			name: "both origins",
			from: from,
			to:   to,
			want: "credentials for https://origin.example were not sent to https://dest.example because a redirect crossed an origin boundary",
		},
		{
			name: "no from",
			to:   to,
			want: "credentials for <nil> were not sent to https://dest.example because a redirect crossed an origin boundary",
		},
		{
			name: "no to",
			from: from,
			want: "credentials for https://origin.example were not sent to <nil> because a redirect crossed an origin boundary",
		},
		{
			name: "neither",
			want: "credentials for <nil> were not sent to <nil> because a redirect crossed an origin boundary",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, (&CredentialsDroppedError{From: tc.from, To: tc.to}).Error())
		})
	}
}
