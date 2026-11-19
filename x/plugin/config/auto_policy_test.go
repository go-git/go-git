package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/config"
)

func TestAutoProtocolPolicyReadsBothGlobalFiles(t *testing.T) { //nolint:paralleltest // mutates process env
	t.Run("XDG restriction applies beside gitconfig", func(t *testing.T) { //nolint:paralleltest // mutates process env
		home := permTestHome(t)
		writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Home\n")
		writeFile(t, filepath.Join(os.Getenv(envXDGConfigHome), "git", "config"),
			"[protocol \"file\"]\n\tallow = never\n")

		assert.Equal(t, config.ProtocolNever, loadFilePolicy(t, NewAuto(), config.GlobalScope))
	})

	t.Run("gitconfig overrides XDG", func(t *testing.T) { //nolint:paralleltest // mutates process env
		home := permTestHome(t)
		writeFile(t, filepath.Join(home, ".gitconfig"), "[protocol \"file\"]\n\tallow = always\n")
		writeFile(t, filepath.Join(os.Getenv(envXDGConfigHome), "git", "config"),
			"[protocol \"file\"]\n\tallow = never\n")

		assert.Equal(t, config.ProtocolAlways, loadFilePolicy(t, NewAuto(), config.GlobalScope))
	})
}
