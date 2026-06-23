package gitcli

import "testing"

func TestSupportsProtocolV2(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		want    bool
	}{
		{"git version 2.18.0", true},
		{"git version 2.54.0", true},
		{"git version 3.0.0", true},
		{"git version 2.18.0.windows.1", true},
		{"git version 2.17.1", false},
		{"git version 2.11.0", false},
		{"git version 1.9.5", false},
		{"git version 2.18\n", true},
		{"git version 2", false},
		{"garbage", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			if got := supportsProtocolV2(tt.version); got != tt.want {
				t.Errorf("supportsProtocolV2(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
