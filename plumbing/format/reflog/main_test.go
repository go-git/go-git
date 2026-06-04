package reflog

import (
	"testing"

	"github.com/go-git/go-git/v6/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunWithLeakCheck(m)
}
