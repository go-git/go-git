package git

import (
	"testing"

	"github.com/go-git/go-git/v6/internal/testutil"
	"github.com/go-git/go-git/v6/internal/trace"
)

func TestMain(m *testing.M) {
	// Set the trace targets based on the environment variables.
	trace.ReadEnv()

	// Register a default ConfigSource so tests that call ConfigScoped
	// (directly or indirectly via Commit/CreateTag) don't fail with
	// "no config loader registered".
	registerTestConfigLoader()

	// Run the tests with leak detection enabled.
	testutil.RunWithLeakCheck(m)
}
