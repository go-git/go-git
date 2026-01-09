package object

import (
	"fmt"
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
