package worktree

import (
	"os"
	"testing"

	testconfig "github.com/go-git/go-git/v6/internal/test/config"
)

func TestMain(m *testing.M) {
	testconfig.RegisterDefault()
	os.Exit(m.Run())
}
