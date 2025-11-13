//go:build !plan9 && !unix && windows
// +build !plan9,!unix,windows

package git

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/go-git/go-billy/v6/util"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preReceiveHook returns the bytes of a pre-receive hook script
// that prints m before exiting successfully
func preReceiveHook(m string) []byte {
	return []byte(fmt.Sprintf("#!C:/Program\\ Files/Git/usr/bin/sh.exe\nprintf '%s'\n", m))
}

func TestCloneFileUrlWindows(t *testing.T) {
	dir := t.TempDir()

	r, err := PlainInit(dir, false)
	require.NoError(t, err)

	err = util.WriteFile(r.wt, "foo", nil, 0755)
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	_, err = w.Add("foo")
	require.NoError(t, err)

	_, err = w.Commit("foo", &CommitOptions{
		Author:    defaultSignature(),
		Committer: defaultSignature(),
	})
	require.NoError(t, err)

	tests := []struct {
		url     string
		pattern string
	}{
		{
			url:     "file://" + filepath.ToSlash(dir),
			pattern: `^file://[A-Z]:/(?:[^/]+/)*\d+$`,
		},
		{
			url:     "file:///" + filepath.ToSlash(dir),
			pattern: `^file:///[A-Z]:/(?:[^/]+/)*\d+$`,
		},
	}

	for _, tc := range tests {
		assert.Regexp(t, regexp.MustCompile(tc.pattern), tc.url)
		_, err = Clone(memory.NewStorage(), nil, &CloneOptions{
			URL: tc.url,
		})

		assert.NoError(t, err, "url: %q", tc.url)
	}
}

func TestCheckFileModeTrustable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	err := os.WriteFile(path, []byte(""), os.ModePerm)
	require.NoError(t, err)

	trust, _ := checkFileModeTrustable(path)
	assert.False(t, trust)
}

func TestPlainInitFileMode(t *testing.T) {
	dir := t.TempDir()
	r, err := PlainInit(dir, false)
	require.NoError(t, err)

	cfg, err := r.Config()
	require.NoError(t, err)
	assert.False(t, cfg.Core.FileMode)
}
