package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasVolumeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		// A drive prefix is a single byte and a colon, whichever
		// byte it is; upstream does not restrict it to A-Z either.
		{`C:foo`, true},
		{`1:foo`, true},
		{`%:x`, true},
		{`x:`, true},
		{`.:x`, true},
		{`::`, true},

		// UNC, Local Device and Root Local Device prefixes.
		{`\\host\share`, true},
		{`//host/share`, true},
		{`\\srv`, true},
		{`\\`, true},
		{`//`, true},
		{`\/`, true},
		{`\\.`, true},
		{`\\?`, true},
		{`\??`, true},
		{`\??\C:\x`, true},
		{`\\?\C:\x`, true},
		{`\\.\UNC\h\s`, true},

		// A colon past the first byte is not a drive prefix, and a
		// single separator is not the start of a UNC path.
		{`a/b:c`, false},
		{`/foo`, false},
		{`foo`, false},
		{`\?`, false},
		{`\`, false},
		{`:`, false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, HasVolumeName(tc.path))
		})
	}
}
