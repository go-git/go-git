package pathutil

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHFSDotDot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"..", true},
		{".\u200c.", true},
		{"\u200c..", true},
		{"..\u200c", true},
		{"\u200c.\u200d.\u200e", true},
		{".", false},
		{"...", false},
		{"....", false},
		{".. ", false},
		{"a..b", false},
		{".git", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsHFSDotDot(tc.name))
		})
	}
}

func TestIsNTFSDotDot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		// A tail of spaces and periods containing at least one space.
		{".. ", true},
		{"..  ", true},
		{".. .", true},
		{"... ", true},
		{".. ..", true},
		// An Alternate Data Stream suffix names the entry itself.
		{"..:x", true},
		{"..:$DATA", true},
		{"..::$INDEX_ALLOCATION", true},
		// The literal name is the job of the == comparison in
		// IsDotOrDotDotName, not of this predicate.
		{"..", false},
		{".", false},
		// Periods alone: NTFS folds these, WindowsValidPath rejects
		// them under core.protectNTFS.
		{"...", false},
		{"....", false},
		// The tail is not spaces and periods alone, so nothing folds.
		{".. x", false},
		{"..x", false},
		{"x..", false},
		{"a..b", false},
		{"foo..", false},
		{"foo", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsNTFSDotDot(tc.name))
		})
	}
}

func TestIsDotOrDotDotName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		// Literal self- and parent-references.
		{".", true},
		{"..", true},

		// NTFS strips trailing spaces from a path component.
		{".. ", true},
		{"..  ", true},
		{".. .", true},
		{"... ", true},
		{".. ..", true},

		// An Alternate Data Stream suffix names the entry itself, so
		// "..:<stream>" is a reference to the parent directory.
		{"..:x", true},
		{"..:$DATA", true},
		{"..::$INDEX_ALLOCATION", true},

		// HFS+ drops ignorable code points during normalisation.
		{".\u200c.", true},
		{"\u200c..", true},
		{"..\u200c", true},
		{"\u200c.\u200d.\u200e", true},

		// A component of periods alone is accepted. NTFS folds it,
		// but it is a legitimate name everywhere else and C Git
		// 2.54.0 accepts it in a tree and in an index on POSIX.
		// WindowsValidPath refuses it under core.protectNTFS.
		{"...", false},
		{"....", false},
		{".....", false},

		// Not a ".." prefix, or a tail that does not fold.
		{"x..", false},
		{"a..b", false},
		{"..x", false},
		{".. x", false},
		{"foo..", false},

		// These fold to "." rather than to "..", so they produce no
		// parent hop. Two tree entries can materialise to the same
		// on-disk name, which is a collision and not an escape, so
		// they are deliberately accepted at every layer.
		{". ", false},
		{". .", false},
		{".\u200c", false},
		{".:x", false},

		// Ordinary names, and names owned by other predicates.
		{"foo", false},
		{".git", false},
		{".gitmodules", false},
		{"CON", false},

		// Callers split with strings.FieldsFunc, which never yields
		// an empty field. Pinned so a caller switching to
		// strings.Split does not silently change meaning.
		{"", false},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.name), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsDotOrDotDotName(tc.name),
				"IsDotOrDotDotName(%q)", tc.name)
		})
	}
}
