package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDotGitName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{".git", true},
		{".GIT", true},
		{".Git", true},
		{".gIt", true},
		{"git~1", true},
		{"GIT~1", true},
		{"Git~1", true},

		{"git", false},
		{"GIT", false},
		{".gitmodules", false},
		{".gitignore", false},
		{"git~", false},
		{"git~10", false},
		{"git~2", false}, // canonical short name is git~1; others are not denied
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsDotGitName(tc.name))
		})
	}
}
