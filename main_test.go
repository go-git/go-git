package git

import (
	"os"
	"testing"

	"github.com/go-git/go-git/v6/internal/trace"
)

func TestMain(m *testing.M) {
	// Set the trace targets based on the environment variables.
	trace.ReadEnv()
	// Run the tests.
	os.Exit(m.Run())
}
