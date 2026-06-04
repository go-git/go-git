// Package testutil provides testing utilities for go-git, including
// optional leak detection for unclosed resources.
package testutil

import (
	"os"
	"testing"
)

// RunWithLeakCheck runs the test suite and triggers leak detection before exit.
// Each package's TestMain should call this function to ensure leak detection
// (enabled with -tags leakcheck) runs after all tests complete.
//
// Example usage:
//
//	func TestMain(m *testing.M) {
//	    testutil.RunWithLeakCheck(m)
//	}
func RunWithLeakCheck(m *testing.M) {
	exitCode := m.Run()
	triggerLeakDetection()
	os.Exit(exitCode)
}
