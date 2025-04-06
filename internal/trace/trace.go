package trace

import (
	"os"
	"strconv"

	"github.com/go-git/go-git/v6/utils/trace"
)

// envToTarget maps what environment variables can be used
// to enable specific trace targets.
var envToTarget = map[string]trace.Target{
	"GIT_TRACE":             trace.General,
	"GIT_TRACE_PACKET":      trace.Packet,
	"GIT_TRACE_SSH":         trace.SSH,
	"GIT_TRACE_PERFORMANCE": trace.Performance,
}

// ReadEnv reads the environment variables and sets the trace targets.
// This is used to enable tracing in the go-git library.
func ReadEnv() {
	var target trace.Target
	for k, v := range envToTarget {
		if val, _ := strconv.ParseBool(os.Getenv(k)); val {
			target |= v
		}
	}
	trace.SetTarget(target)
}
