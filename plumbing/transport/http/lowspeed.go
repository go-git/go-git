package http

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// LowSpeedGuard configures a minimum receive throughput guard for HTTP
// transfers, mirroring git's http.lowSpeedLimit/http.lowSpeedTime (and curl's
// CURLOPT_LOW_SPEED_LIMIT/CURLOPT_LOW_SPEED_TIME). A transfer is aborted when
// either a single read blocks longer than Time without delivering data (a full
// stall) or the average receive speed while blocked in Read stays below Limit
// bytes per second over a full Time window (a sustained trickle). Both fields
// must be positive for the guard to take effect.
type LowSpeedGuard struct {
	// Limit is the minimum receive speed in bytes per second. A value of zero
	// or less disables the guard.
	Limit int64
	// Time is the window over which the average speed is measured. A zero or
	// negative value disables the guard.
	Time time.Duration
}

// valid reports whether the guard is configured and should be applied.
func (g *LowSpeedGuard) valid() bool {
	return g != nil && g.Limit > 0 && g.Time > 0
}

// lowSpeedBody wraps a response body to enforce a minimum receive throughput.
//
// Unlike a connection-layer guard, the watchdog lives entirely on the response
// body go-git owns: it never touches net/http's connection pool, so it does not
// shorten idle keep-alive lifetimes or poison multiplexed HTTP/2 connections,
// and it works even when the caller supplies their own *http.Client. Because it
// only accumulates time actually spent blocked in Read, pauses between reads
// (e.g. while go-git decodes a packfile object) do not count against the floor.
//
// Bounding every read is what makes draining a stalled response body safe: the
// drain in httpResponseBody.DrainClose cannot hang indefinitely while the guard
// is configured, even when the request context carries no deadline. When the
// guard is unset the drain is unbounded — matching git's off-by-default posture.
type lowSpeedBody struct {
	rc     io.ReadCloser
	limit  int64         // minimum bytes per second
	window time.Duration // measured over this period

	active time.Duration // time blocked in Read since the window opened
	count  int64         // bytes read since the window opened

	timer   *time.Timer
	aborted atomic.Bool
}

func newLowSpeedBody(rc io.ReadCloser, guard *LowSpeedGuard) *lowSpeedBody {
	b := &lowSpeedBody{rc: rc, limit: guard.Limit, window: guard.Time}
	// Create the timer in a stopped state; it will be Reset before each Read.
	b.timer = time.AfterFunc(1<<63-1, b.trip)
	b.timer.Stop()
	return b
}

func (b *lowSpeedBody) Read(p []byte) (int, error) {
	if b.aborted.Load() {
		return 0, b.speedErr()
	}

	// Arm a stall bound: this read may not block longer than window without
	// delivering data. The timer fires trip() from another goroutine, which
	// closes the underlying body and unblocks a stalled Read.
	b.timer.Reset(b.window)
	start := time.Now()
	n, err := b.rc.Read(p)
	b.timer.Stop()

	b.active += time.Since(start)
	b.count += int64(n)
	if b.active >= b.window {
		if float64(b.count)/b.active.Seconds() < float64(b.limit) {
			b.trip()
		}
		b.active, b.count = 0, 0
	}

	if b.aborted.Load() {
		return n, b.speedErr()
	}
	return n, err
}

func (b *lowSpeedBody) Close() error {
	b.timer.Stop()
	return b.rc.Close()
}

// trip marks the transfer aborted and closes the underlying body exactly once,
// interrupting any in-flight Read.
func (b *lowSpeedBody) trip() {
	if b.aborted.CompareAndSwap(false, true) {
		_ = b.rc.Close()
	}
}

func (b *lowSpeedBody) speedErr() error {
	return fmt.Errorf("http transport: transfer speed below %d bytes/sec over %s", b.limit, b.window)
}
