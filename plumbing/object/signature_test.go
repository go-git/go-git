package object

import (
	"bytes"
	"testing"
)

func Test_typeForSignature(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
		want signatureType
	}{
		{
			name: "known signature format (SSH)",
			b: []byte(`-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
0D7wOhjWVbYZH6KugAAAADZ2l0AAAAAAAAAAZzaGE1MTIAAABTAAAAC3NzaC1lZDI1NTE5
AAAAQIYHMhSVV9L2xwJuV8eWMLjThya8yXgCHDzw3p01D19KirrabW0veiichPB5m+Ihtr
MKEQruIQWJb+8HVXwssA4=
-----END SSH SIGNATURE-----`),
			want: signatureTypeSSH,
		},
		{
			name: "known signature format (X509)",
			b: []byte(`-----BEGIN CERTIFICATE-----
MIIDZjCCAk6gAwIBAgIJALZ9Z3Z9Z3Z9MA0GCSqGSIb3DQEBCwUAMIGIMQswCQYD
VQQGEwJTRTEOMAwGA1UECAwFVGV4YXMxDjAMBgNVBAcMBVRleGFzMQ4wDAYDVQQK
DAVUZXhhczEOMAwGA1UECwwFVGV4YXMxGDAWBgNVBAMMD1RleGFzIENlcnRpZmlj
YXRlMB4XDTE3MDUyNjE3MjY0MloXDTI3MDUyNDE3MjY0MlowgYgxCzAJBgNVBAYT
AlNFMQ4wDAYDVQQIDAVUZXhhczEOMAwGA1UEBwwFVGV4YXMxDjAMBgNVBAoMBVRl
eGFzMQ4wDAYDVQQLDAVUZXhhczEYMBYGA1UEAwwPVGV4YXMgQ2VydGlmaWNhdGUw
ggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDQZ9Z3Z9Z3Z9Z3Z9Z3Z9Z3
-----END CERTIFICATE-----`),
			want: signatureTypeX509,
		},
		{
			name: "unknown signature format",
			b: []byte(`-----BEGIN ARBITRARY SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
-----END UNKNOWN SIGNATURE-----`),
			want: signatureTypeUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := typeForSignature(tt.b); got != tt.want {
				t.Errorf("typeForSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseSignedBytes(t *testing.T) {
	tests := []struct {
		name          string
		b             []byte
		wantSignature []byte
		wantType      signatureType
	}{
		{
			name: "signature with trailing data",
			b: []byte(`An invalid

-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
0D7wOhjWVbYZH6KugAAAADZ2l0AAAAAAAAAAZzaGE1MTIAAABTAAAAC3NzaC1lZDI1NTE5
AAAAQIYHMhSVV9L2xwJuV8eWMLjThya8yXgCHDzw3p01D19KirrabW0veiichPB5m+Ihtr
MKEQruIQWJb+8HVXwssA4=
-----END SSH SIGNATURE-----

signed tag`),
			wantSignature: []byte(`-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
0D7wOhjWVbYZH6KugAAAADZ2l0AAAAAAAAAAZzaGE1MTIAAABTAAAAC3NzaC1lZDI1NTE5
AAAAQIYHMhSVV9L2xwJuV8eWMLjThya8yXgCHDzw3p01D19KirrabW0veiichPB5m+Ihtr
MKEQruIQWJb+8HVXwssA4=
-----END SSH SIGNATURE-----

signed tag`),
			wantType: signatureTypeSSH,
		},
		{
			name:          "data without signature",
			b:             []byte(`Some message`),
			wantSignature: []byte(``),
			wantType:      signatureTypeUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, st := parseSignedBytes(tt.b)
			var signature []byte
			if pos >= 0 {
				signature = tt.b[pos:]
			}
			if !bytes.Equal(signature, tt.wantSignature) {
				t.Errorf("parseSignedBytes() got = %s for pos = %v, want %s", signature, pos, tt.wantSignature)
			}
			if st != tt.wantType {
				t.Errorf("parseSignedBytes() got1 = %v, want %v", st, tt.wantType)
			}
		})
	}
}
