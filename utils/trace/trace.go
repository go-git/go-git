package trace

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

func init() {
	var target Target
	for k, v := range envToTarget {
		if strings.EqualFold(os.Getenv(k), "true") {
			target |= v
		}
	}
	SetTarget(target)
}

var (
	// logger is the logger to use for tracing.
	logger = newLogger()

	// current is the targets that are enabled for tracing.
	current atomic.Int32

	// envToTarget maps what environment variables can be used
	// to enable specific trace targets.
	envToTarget = map[string]Target{
		"GIT_TRACE":             General,
		"GIT_TRACE_PACKET":      Packet,
		"GIT_TRACE_SSH":         SSH,
		"GIT_TRACE_PERFORMANCE": Performance,
	}
)

func newLogger() *log.Logger {
	return log.New(os.Stderr, "", log.Ltime|log.Lmicroseconds|log.Lshortfile)
}

// Target is a tracing target.
type Target int32

const (
	// General traces general operations.
	General Target = 1 << iota

	// Packet traces git packets.
	Packet

	// SSH traces SSH handshake operations. This does not have
	// a direct translation to an upstream trace option.
	SSH

	// Performance traces performance of go-git components.
	Performance
)

// SetTarget sets the tracing targets.
func SetTarget(target Target) {
	current.Store(int32(target))
}

// SetLogger sets the logger to use for tracing.
func SetLogger(l *log.Logger) {
	logger = l
}

// Print prints the given message only if the target is enabled.
func (t Target) Print(args ...interface{}) {
	if t.Enabled() {
		logger.Output(2, fmt.Sprint(args...)) // nolint: errcheck
	}
}

// Printf prints the given message only if the target is enabled.
func (t Target) Printf(format string, args ...interface{}) {
	if t.Enabled() {
		logger.Output(2, fmt.Sprintf(format, args...)) // nolint: errcheck
	}
}

// Enabled returns true if the target is enabled.
func (t Target) Enabled() bool {
	return int32(t)&current.Load() != 0
}
