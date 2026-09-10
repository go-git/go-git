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
		// Periods alone pass the tree gate; Win32ValidPath rejects them.
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

		// Periods alone pass this predicate. validPath applies the
		// Win32 trailing rule only on Windows with core.protectNTFS.
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

func TestIsDotsOnlyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{".", true},
		{"..", true},
		{"...", true},
		{"....", true},
		{".....", true},
		{"", false},
		{". ", false},
		{" .", false},
		{".a", false},
		{"a.", false},
		{"a..b", false},
		{".\u200c.", false},
		{"..:x", false},
		{"foo", false},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.name), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsDotsOnlyName(tc.name))
		})
	}
}

// Reference names already rejected literal dot; submodule names gain it.
func TestIsDotsOnlyNameComplementsIsDotOrDotDotName(t *testing.T) {
	t.Parallel()
	for _, part := range generateComponents(t, ". :a~gt$\u200c", 4) {
		got := IsDotsOnlyName(part) || IsDotOrDotDotName(part)
		reference := part == "." || IsHFSDot(part, ".") || IsNTFSDot(part, ".", "")
		submodule := IsHFSDot(part, ".") || IsNTFSDot(part, ".", "")
		assert.Equal(t, reference, got, "reference component %q", part)
		assert.Equal(t, part == ".", got != submodule, "submodule delta %q", part)
	}
}

// generateComponents returns every string of length 0 to maxLen over
// the runes of alphabet. It exists so the two cross-checks in this
// package can assert an invariant over a closed corpus rather than
// over a hand-picked table.
func generateComponents(t *testing.T, alphabet string, maxLen int) []string {
	t.Helper()

	out := make([]string, 1, 1+len(alphabet)*maxLen)
	cur := []string{""}
	for range maxLen {
		next := make([]string, 0, len(alphabet)*len(cur))
		for _, prefix := range cur {
			for _, r := range alphabet {
				next = append(next, prefix+string(r))
			}
		}
		out = append(out, next...)
		cur = next
	}
	return out
}
