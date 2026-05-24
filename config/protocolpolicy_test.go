package config

import (
	"testing"
)

func TestValidateProtocolPolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"always", false},
		{"ALWAYS", false},
		{"never", false},
		{"user", false},
		{"User", false},
		{"", false}, // empty is permitted; means "use the resolution chain"
		{"sometimes", true},
		{"alwys", true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			err := ValidateProtocolPolicy("protocol.allow", tc.value)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateProtocolPolicy(%q) err=%v wantErr=%v", tc.value, err, tc.wantErr)
			}
		})
	}
}
