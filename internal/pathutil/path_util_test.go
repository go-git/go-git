package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasUnsafeComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want bool
	}{
		{"refs/heads/main", false},
		{"refs/heads/feature/nested", false},
		{"HEAD", false},
		{"", false},
		{"refs/heads/a.b", false},
		{"refs/heads/.hidden", false},
		{"refs/tags/v1.0.0", false},

		{".", true},
		{"..", true},
		{"refs/../config", true},
		{"refs/heads/../../config", true},
		{"refs/./heads", true},
		{"refs\\..\\config", true},

		// NTFS strips trailing periods and spaces from a component and
		// truncates it at the Alternate Data Stream colon, so each of these
		// resolves to "..".
		{"refs/...", true},
		{"refs/.. /config", true},
		{"refs/..:$DATA/config", true},

		// HFS+ drops these code points during path normalisation, so the
		// component reads as ".." to the filesystem while holding no literal
		// "..": zero width non-joiner, the BOM, and the left-to-right mark.
		{"refs/\u200c.\u200c./config", true},
		{"refs/\ufeff.\ufeff.\ufeff/config", true},
		{"refs/\u200e.\u200e./config", true},

		// A component the filesystem reads as a single "." is an alias for the
		// directory it sits in, so each of these names resolves to
		// "refs/heads/main". Nothing after the dot spells the component, so
		// the needle for these is the empty string.
		{"refs/heads/\u200c./main", true},
		{"refs/heads/.\u200c/main", true},
		{"refs/heads/\u200c.\u200c/main", true},
		{"refs/\ufeff./heads/main", true},
		{"refs/\u200e./heads/main", true},
		{"refs/heads/. /main", true},
		{"refs/heads/.  /main", true},
		{"refs/heads/.:$DATA/main", true},
		{"refs/heads/. :$DATA/main", true},

		{"refs/heads/ma\x00in", true},
		{"refs/heads/ma\nin", true},
		{"refs/heads/ma\x7fin", true},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, HasUnsafeComponent(tc.in))
		})
	}
}
