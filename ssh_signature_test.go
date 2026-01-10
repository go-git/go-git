package git

import (
	"strings"
	"testing"
)

// Valid SSH signature generated with: ssh-keygen -Y sign -f key -n git < message
const testValidSSHSignature = `-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
0D7wOhjWVbYZH6KugAAAADZ2l0AAAAAAAAAAZzaGE1MTIAAABTAAAAC3NzaC1lZDI1NTE5
AAAAQIYHMhSVV9L2xwJuV8eWMLjThya8yXgCHDzw3p01D19KirrabW0veiichPB5m+Ihtr
MKEQruIQWJb+8HVXwssA4=
-----END SSH SIGNATURE-----`

func TestParseSSHSignature(t *testing.T) {
	t.Parallel()

	sig, err := parseSSHSignature([]byte(testValidSSHSignature))
	if err != nil {
		t.Fatalf("failed to parse signature: %v", err)
	}

	if sig.Version != 1 {
		t.Errorf("expected version 1, got %d", sig.Version)
	}
	if sig.Namespace != "git" {
		t.Errorf("expected namespace 'git', got %q", sig.Namespace)
	}
	if sig.HashAlgorithm != "sha512" {
		t.Errorf("expected hash algorithm 'sha512', got %q", sig.HashAlgorithm)
	}
	if sig.PublicKey == nil {
		t.Error("expected non-nil public key")
	}
	if sig.Signature == nil {
		t.Error("expected non-nil signature")
	}
}

func TestParseSSHSignature_Fingerprint(t *testing.T) {
	t.Parallel()

	sig, err := parseSSHSignature([]byte(testValidSSHSignature))
	if err != nil {
		t.Fatalf("failed to parse signature: %v", err)
	}

	fingerprint := sig.Fingerprint()
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Errorf("expected fingerprint starting with SHA256:, got %q", fingerprint)
	}
}

func TestParseSSHSignature_InvalidArmor(t *testing.T) {
	t.Parallel()

	_, err := parseSSHSignature([]byte("not a signature"))
	if err == nil {
		t.Error("expected error for invalid armor")
	}
}

func TestParseSSHSignature_InvalidBase64(t *testing.T) {
	t.Parallel()

	sig := "-----BEGIN SSH SIGNATURE-----\n!!!invalid!!!\n-----END SSH SIGNATURE-----"
	_, err := parseSSHSignature([]byte(sig))
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestParseSSHSignature_InvalidMagic(t *testing.T) {
	t.Parallel()

	// Valid base64 but wrong magic
	sig := "-----BEGIN SSH SIGNATURE-----\naW52YWxpZA==\n-----END SSH SIGNATURE-----"
	_, err := parseSSHSignature([]byte(sig))
	if err == nil {
		t.Error("expected error for invalid magic")
	}
	if !strings.Contains(err.Error(), "magic") {
		t.Errorf("expected magic error, got: %v", err)
	}
}

func TestParseSSHSignature_TooShort(t *testing.T) {
	t.Parallel()

	// Valid armor but data too short
	sig := "-----BEGIN SSH SIGNATURE-----\nYWJj\n-----END SSH SIGNATURE-----"
	_, err := parseSSHSignature([]byte(sig))
	if err == nil {
		t.Error("expected error for too short data")
	}
}

func FuzzParseSSHSignature(f *testing.F) {
	f.Add([]byte(testValidSSHSignature))
	f.Add([]byte("-----BEGIN SSH SIGNATURE-----\n-----END SSH SIGNATURE-----"))
	f.Add([]byte("random data"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		// Should not panic
		parseSSHSignature(data)
	})
}
