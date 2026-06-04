package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHFSDotGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		part string
		want bool
	}{
		{".git", true},
		{".Git", true},
		{".GIT", true},
		{".gIt", true},
		{".g\u200cit", true},
		{".gi\u200dt", true},
		{".gi\ufefft", true},
		{"\u200e.git", true},
		{".g\u200ci\u200dt", true},
		{".gitmodules", false},
		{".gitignore", false},
		{".git2", false},
		{"git", false},
		{".gxt", false},
		{"", false},
		{".", false},
		{".g\x80it", false},
	}

	for _, tc := range tests {
		t.Run(tc.part, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsHFSDotGit(tc.part))
		})
	}
}

func TestIsHFSDot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		part   string
		needle string
		want   bool
	}{
		// .git \u2014 matches existing IsHFSDotGit semantics.
		{"plain .git", ".git", "git", true},
		{"zwj in .git", ".g\u200dit", "git", true},
		{"zwnj in .git", ".g\u200cit", "git", true},
		{"leading lrm", "\u200e.git", "git", true},
		{".gitmodules is not .git", ".gitmodules", "git", false},
		{"empty string for git", "", "git", false},

		// .gitmodules \u2014 the parameterised needle.
		{"plain .gitmodules", ".gitmodules", "gitmodules", true},
		{"uppercase .GITMODULES", ".GITMODULES", "gitmodules", true},
		{"zwnj in .gitmodules", ".g\u200citmodules", "gitmodules", true},
		{"zwj in .gitmodules", ".gitmod\u200dules", "gitmodules", true},
		{"trailing zwsp", ".gitmodules\ufeff", "gitmodules", true},
		{"leading lrm", "\u200e.gitmodules", "gitmodules", true},
		{".gitmodulesfoo", ".gitmodulesfoo", "gitmodules", false},
		{".git is not .gitmodules", ".git", "gitmodules", false},
		{"plain readme.md", "readme.md", "gitmodules", false},
		{"empty string", "", "gitmodules", false},
		{"only dot", ".", "gitmodules", false},
		{"non-ascii in needle position", ".g\x80itmodules", "gitmodules", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsHFSDot(tc.part, tc.needle)
			assert.Equal(t, tc.want, got, "IsHFSDot(%q, %q)", tc.part, tc.needle)
		})
	}
}

func TestIsHFSDotGitmodules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		part string
		want bool
	}{
		{".gitmodules", true},
		{".GITMODULES", true},
		{".g\u200citmodules", true},
		{".gitmod\u200dules", true},
		{"\u200e.gitmodules", true},
		{".gitmodules\ufeff", true},
		{".gitmodulesfoo", false},
		{".git", false},
		{"readme.md", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.part, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsHFSDotGitmodules(tc.part))
		})
	}
}

func TestIsHFSDotMetadataFamily(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		fn   func(string) bool
		hit  string
	}{
		{"IsHFSDotGitattributes", IsHFSDotGitattributes, ".gitattributes"},
		{"IsHFSDotGitignore", IsHFSDotGitignore, ".gitignore"},
		{"IsHFSDotMailmap", IsHFSDotMailmap, ".mailmap"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Canonical name and one zero-width variant hit.
			assert.True(t, tc.fn(tc.hit), "expected %s(%q) to be true", tc.name, tc.hit)
			disguised := tc.hit[:2] + "\u200c" + tc.hit[2:]
			assert.True(t, tc.fn(disguised), "expected %s(%q) to be true", tc.name, disguised)
			// Unrelated names miss.
			assert.False(t, tc.fn(".gitmodules"), "expected %s(%q) to be false", tc.name, ".gitmodules")
			assert.False(t, tc.fn(""), "expected %s(empty) to be false", tc.name)
		})
	}
}
