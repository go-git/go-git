package object

import (
	"bytes"
	"strings"
	"testing"
)

func Test_lastSignatureBlockOffset(t *testing.T) {
	t.Parallel()
	// lastSignatureBlockOffset must agree with parseSignedBytes on every input,
	// including a message body longer than the bufio buffer (which forces the
	// ReadSlice ErrBufferFull path).
	inputs := [][]byte{
		[]byte("Some message"),
		[]byte("signed tag\n-----BEGIN PGP SIGNATURE-----\n\nx\n-----END PGP SIGNATURE-----"),
		[]byte("msg\n-----BEGIN PGP SIGNATURE-----\na\n-----END PGP SIGNATURE-----\n" +
			"-----BEGIN SSH SIGNATURE-----\nb\n-----END SSH SIGNATURE-----"),
		[]byte(strings.Repeat("a", 70000) + "\n-----BEGIN PGP SIGNATURE-----\nx\n-----END PGP SIGNATURE-----"),
		[]byte("-----BEGIN PGP SIGNATURE-----\nonly\n-----END PGP SIGNATURE-----"),
		[]byte(""),
	}
	for i, in := range inputs {
		got, err := lastSignatureBlockOffset(bytes.NewReader(in))
		if err != nil {
			t.Fatalf("input %d: %v", i, err)
		}
		if want := parseSignedBytes(in); got != want {
			t.Errorf("input %d: lastSignatureBlockOffset() = %d, want %d", i, got, want)
		}
	}
}

func Test_isSignatureStart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		b    []byte
		want bool
	}{
		{
			name: "known signature format (PGP)",
			b: []byte(`-----BEGIN PGP SIGNATURE-----

iHUEABYKAB0WIQTMqU0ycQ3f6g3PMoWMmmmF4LuV8QUCYGebVwAKCRCMmmmF4LuV
8VtyAP9LbuXAhtK6FQqOjKybBwlV70rLcXVP24ubDuz88VVwSgD+LuObsasWq6/U
TssDKHUR2taa53bQYjkZQBpvvwOrLgc=
=YQUf
-----END PGP SIGNATURE-----`),
			want: true,
		},
		{
			name: "known signature format (SSH)",
			b: []byte(`-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
-----END SSH SIGNATURE-----`),
			want: true,
		},
		{
			name: "known signature format (X.509)",
			b: []byte(`-----BEGIN SIGNED MESSAGE-----
MIIDZjCCAk6gAwIBAgIJALZ9Z3Z9Z3Z9MA0GCSqGSIb3DQEBCwUAMIGIMQswCQYD
-----END SIGNED MESSAGE-----`),
			want: true,
		},
		{
			name: "unknown signature format",
			b: []byte(`-----BEGIN ARBITRARY SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
-----END UNKNOWN SIGNATURE-----`),
			want: false,
		},
		{
			name: "unknown signature format CERTIFICATE",
			b: []byte(`-----BEGIN CERTIFICATE-----
MIIDZjCCAk6gAwIBAgIJALZ9Z3Z9Z3Z9MA0GCSqGSIb3DQEBCwUAMIGIMQswCQYD
-----END CERTIFICATE-----`),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isSignatureStart(tt.b); got != tt.want {
				t.Errorf("isSignatureStart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseSignedBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		b             []byte
		wantSignature []byte
	}{
		{
			name: "detects signature",
			b: []byte(`signed tag
-----BEGIN PGP SIGNATURE-----

iQGzBAABCAAdFiEE/h5sbbqJFh9j1AdUSqtFFGopTmwFAmB5XFkACgkQSqtFFGop
sZC//k6m
=VhHy
-----END PGP SIGNATURE-----`),
			wantSignature: []byte(`-----BEGIN PGP SIGNATURE-----

iQGzBAABCAAdFiEE/h5sbbqJFh9j1AdUSqtFFGopTmwFAmB5XFkACgkQSqtFFGop
sZC//k6m
=VhHy
-----END PGP SIGNATURE-----`),
		},
		{
			name: "last signature for multiple signatures",
			b: []byte(`signed tag
-----BEGIN PGP SIGNATURE-----

sZC//k6m
=VhHy
-----END PGP SIGNATURE-----
-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
-----END SSH SIGNATURE-----`),
			wantSignature: []byte(`-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
-----END SSH SIGNATURE-----`),
		},
		{
			name: "signature with trailing data",
			b: []byte(`An invalid

-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
-----END SSH SIGNATURE-----

signed tag`),
			wantSignature: []byte(`-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
-----END SSH SIGNATURE-----

signed tag`),
		},
		{
			name:          "data without signature",
			b:             []byte(`Some message`),
			wantSignature: []byte(``),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := parseSignedBytes(tt.b)
			var signature []byte
			if pos >= 0 {
				signature = tt.b[pos:]
			}
			if !bytes.Equal(signature, tt.wantSignature) {
				t.Errorf("parseSignedBytes() got = %s for pos = %v, want %s", signature, pos, tt.wantSignature)
			}
		})
	}
}

func FuzzParseSignedBytes(f *testing.F) {
	for _, begin := range signatureBegins {
		f.Add(begin)
	}

	f.Fuzz(func(_ *testing.T, input []byte) {
		parseSignedBytes(input)
	})
}
