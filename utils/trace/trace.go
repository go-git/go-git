// Package trace provides tracing utilities for debugging go-git operations.
package trace

import (
	"fmt"
	"log"
	"os"
	"sync/atomic"
)

var (
	// logger is the logger to use for tracing.
	logger = newLogger()

	// current is the targets that are enabled for tracing.
	current atomic.Int32
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

	// HTTP traces HTTP operations and requests.
	HTTP
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
func (t Target) Print(args ...any) {
	if t.Enabled() {
		logger.Output(2, fmt.Sprint(args...)) // nolint: errcheck
	}
}

// Printf prints the given message only if the target is enabled.
func (t Target) Printf(format string, args ...any) {
	if t.Enabled() {
		logger.Output(2, fmt.Sprintf(format, args...)) // nolint: errcheck
	}
}

// Enabled returns true if the target is enabled.
func (t Target) Enabled() bool {
	return int32(t)&current.Load() != 0
}

// GetTarget returns the current tracing target.
func GetTarget() Target {
	return Target(current.Load())
}
