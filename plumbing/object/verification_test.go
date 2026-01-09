package object

import (
	"errors"
	"fmt"
	"strings"
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
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.level.String(); got != tt.want {
				t.Errorf("TrustLevel.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrustLevel_AtLeast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level    TrustLevel
		required TrustLevel
		want     bool
	}{
		{TrustUltimate, TrustFull, true},
		{TrustFull, TrustFull, true},
		{TrustMarginal, TrustFull, false},
		{TrustNever, TrustMarginal, false},
		{TrustUndefined, TrustNever, false},
		{TrustFull, TrustMarginal, true},
		{TrustMarginal, TrustMarginal, true},
		{TrustUltimate, TrustUndefined, true},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s_AtLeast_%s", tt.level, tt.required)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tt.level.AtLeast(tt.required); got != tt.want {
				t.Errorf("TrustLevel.AtLeast() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerificationResult_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result VerificationResult
		want   bool
	}{
		{
			name:   "valid signature",
			result: VerificationResult{Valid: true, Error: nil},
			want:   true,
		},
		{
			name:   "invalid signature with error",
			result: VerificationResult{Valid: false, Error: errors.New("bad sig")},
			want:   false,
		},
		{
			name:   "valid but with error",
			result: VerificationResult{Valid: true, Error: errors.New("some error")},
			want:   false,
		},
		{
			name:   "invalid without error",
			result: VerificationResult{Valid: false, Error: nil},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.result.IsValid(); got != tt.want {
				t.Errorf("VerificationResult.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerificationResult_IsTrusted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		result   VerificationResult
		minTrust TrustLevel
		want     bool
	}{
		{
			name:     "trusted with full trust",
			result:   VerificationResult{Valid: true, TrustLevel: TrustFull},
			minTrust: TrustMarginal,
			want:     true,
		},
		{
			name:     "not trusted - below threshold",
			result:   VerificationResult{Valid: true, TrustLevel: TrustMarginal},
			minTrust: TrustFull,
			want:     false,
		},
		{
			name:     "not trusted - invalid signature",
			result:   VerificationResult{Valid: false, TrustLevel: TrustUltimate},
			minTrust: TrustMarginal,
			want:     false,
		},
		{
			name:     "not trusted - valid with error",
			result:   VerificationResult{Valid: true, TrustLevel: TrustUltimate, Error: errors.New("err")},
			minTrust: TrustMarginal,
			want:     false,
		},
		{
			name:     "exact trust level match",
			result:   VerificationResult{Valid: true, TrustLevel: TrustFull},
			minTrust: TrustFull,
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.result.IsTrusted(tt.minTrust); got != tt.want {
				t.Errorf("VerificationResult.IsTrusted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerificationResult_String(t *testing.T) {
	t.Parallel()
	result := VerificationResult{
		Type:                  SignatureTypeOpenPGP,
		Valid:                 true,
		TrustLevel:            TrustFull,
		KeyID:                 "ABCD1234EFGH5678",
		PrimaryKeyFingerprint: "ABCD1234EFGH5678IJKL9012MNOP3456QRST7890",
		Signer:                "Test User <test@example.com>",
	}

	s := result.String()
	if !strings.Contains(s, "openpgp") {
		t.Errorf("String() should contain signature type, got: %s", s)
	}
	if !strings.Contains(s, "valid") {
		t.Errorf("String() should contain validity, got: %s", s)
	}
	if !strings.Contains(s, "full") {
		t.Errorf("String() should contain trust level, got: %s", s)
	}
	if !strings.Contains(s, "ABCD1234EFGH5678") {
		t.Errorf("String() should contain key ID, got: %s", s)
	}
	if !strings.Contains(s, "Test User") {
		t.Errorf("String() should contain signer, got: %s", s)
	}
}

func TestVerificationResult_String_Invalid(t *testing.T) {
	t.Parallel()
	result := VerificationResult{
		Type:  SignatureTypeSSH,
		Valid: false,
	}

	s := result.String()
	if !strings.Contains(s, "invalid") {
		t.Errorf("String() should contain 'invalid' for invalid signature, got: %s", s)
	}
	if !strings.Contains(s, "ssh") {
		t.Errorf("String() should contain signature type, got: %s", s)
	}
}
