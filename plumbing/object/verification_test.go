package object

import (
	"testing"
)

func TestTrustLevel_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level TrustLevel
		want  string
	}{
		{TrustUndefined, "undefined"},
		{TrustNever, "never"},
		{TrustMarginal, "marginal"},
		{TrustFull, "full"},
		{TrustUltimate, "ultimate"},
		{TrustLevel(99), "undefined"},
	}

	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("TrustLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestTrustLevel_AtLeast(t *testing.T) {
	t.Parallel()

	if !TrustFull.AtLeast(TrustMarginal) {
		t.Error("Full should be at least Marginal")
	}
	if !TrustFull.AtLeast(TrustFull) {
		t.Error("Full should be at least Full")
	}
	if TrustMarginal.AtLeast(TrustFull) {
		t.Error("Marginal should not be at least Full")
	}
}

func TestVerificationResult_IsValid(t *testing.T) {
	t.Parallel()

	valid := &VerificationResult{Valid: true}
	if !valid.IsValid() {
		t.Error("expected IsValid() to return true")
	}

	invalid := &VerificationResult{Valid: false}
	if invalid.IsValid() {
		t.Error("expected IsValid() to return false")
	}

	validWithError := &VerificationResult{Valid: true, Error: ErrNoSignature}
	if validWithError.IsValid() {
		t.Error("expected IsValid() to return false when Error is set")
	}
}

func TestVerificationResult_IsTrusted(t *testing.T) {
	t.Parallel()

	trusted := &VerificationResult{Valid: true, TrustLevel: TrustFull}
	if !trusted.IsTrusted(TrustMarginal) {
		t.Error("expected IsTrusted(Marginal) to be true for Full trust")
	}
	if !trusted.IsTrusted(TrustFull) {
		t.Error("expected IsTrusted(Full) to be true for Full trust")
	}
	if trusted.IsTrusted(TrustUltimate) {
		t.Error("expected IsTrusted(Ultimate) to be false for Full trust")
	}

	invalid := &VerificationResult{Valid: false, TrustLevel: TrustUltimate}
	if invalid.IsTrusted(TrustUndefined) {
		t.Error("expected IsTrusted to be false for invalid signature regardless of trust")
	}
}

func TestVerificationResult_String(t *testing.T) {
	t.Parallel()

	result := &VerificationResult{
		Type:       SignatureTypeOpenPGP,
		Valid:      true,
		TrustLevel: TrustFull,
		KeyID:      "ABC123",
		Signer:     "test <test@example.com>",
	}
	s := result.String()
	if s != "OpenPGP signature: valid, trust: full, key: ABC123, signer: test <test@example.com>" {
		t.Errorf("unexpected String(): %s", s)
	}
}

func TestSignatureType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		st   SignatureType
		want string
	}{
		{SignatureTypeUnknown, "unknown"},
		{SignatureTypeOpenPGP, "OpenPGP"},
		{SignatureTypeX509, "X.509"},
		{SignatureTypeSSH, "SSH"},
	}

	for _, tt := range tests {
		if got := tt.st.String(); got != tt.want {
			t.Errorf("SignatureType(%d).String() = %q, want %q", tt.st, got, tt.want)
		}
	}
}

func TestDetectSignatureType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sig  []byte
		want SignatureType
	}{
		{"OpenPGP", []byte("-----BEGIN PGP SIGNATURE-----\ndata"), SignatureTypeOpenPGP},
		{"SSH", []byte("-----BEGIN SSH SIGNATURE-----\ndata"), SignatureTypeSSH},
		{"X509 SIGNED", []byte("-----BEGIN SIGNED MESSAGE-----\ndata"), SignatureTypeX509},
		{"X509 CERT", []byte("-----BEGIN CERTIFICATE-----\ndata"), SignatureTypeX509},
		{"PGP MESSAGE", []byte("-----BEGIN PGP MESSAGE-----\ndata"), SignatureTypeOpenPGP},
		{"Unknown", []byte("not a signature"), SignatureTypeUnknown},
		{"Empty", []byte{}, SignatureTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DetectSignatureType(tt.sig); got != tt.want {
				t.Errorf("DetectSignatureType() = %v, want %v", got, tt.want)
			}
		})
	}
}
