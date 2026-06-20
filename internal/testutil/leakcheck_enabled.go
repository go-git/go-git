//go:build leakcheck

package testutil

import (
	"runtime"
	"runtime/debug"
	"time"
)

// TriggerLeakDetection forces garbage collection and waits for finalizers to
// run, ensuring that any leaked repositories or storage instances are detected
// before the test suite exits.
//
// This function is automatically called by RunWithLeakCheck, but can also be
// called directly by custom TestMain implementations that need to perform
// cleanup before exit.
func TriggerLeakDetection() {
	// Free memory to make objects eligible for collection
	debug.FreeOSMemory()

	// Create a sentinel object with a finalizer to detect when finalizers run
	done := make(chan bool, 1)
	sentinel := new(int)
	runtime.SetFinalizer(sentinel, func(*int) {
		done <- true
	})

	// Make sentinel unreachable
	sentinel = nil

	// Force garbage collection multiple times
	for i := 0; i < 5; i++ {
		runtime.GC()
		runtime.Gosched()
	}

	// Wait for the sentinel finalizer to run (with timeout)
	select {
	case <-done:
		// Finalizers have started running, give them more time to complete
		time.Sleep(100 * time.Millisecond)
	case <-time.After(2 * time.Second):
		// Timeout waiting for finalizers - they may not run at all
		println("WARNING: Finalizers did not run within 2 seconds")
	}

	// Final GC to catch any finalizers that created new objects
	runtime.GC()
	runtime.Gosched()
}

// For internal use by RunWithLeakCheck
func triggerLeakDetection() {
	TriggerLeakDetection()
}
