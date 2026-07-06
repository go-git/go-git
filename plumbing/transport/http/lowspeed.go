package http

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// lowSpeedBody wraps a response body to enforce a minimum receive throughput,
// mirroring git's http.lowSpeedLimit/http.lowSpeedTime (and curl's
// CURLOPT_LOW_SPEED_LIMIT/CURLOPT_LOW_SPEED_TIME). It aborts a transfer when
// either a single read blocks longer than window without delivering data (a
// full stall) or the average receive speed while blocked in Read stays below
// limit bytes per second over a full window (a sustained trickle).
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

	aborted atomic.Bool
}

func newLowSpeedBody(rc io.ReadCloser, limit int64, window time.Duration) *lowSpeedBody {
	return &lowSpeedBody{rc: rc, limit: limit, window: window}
}

func (b *lowSpeedBody) Read(p []byte) (int, error) {
	if b.aborted.Load() {
		return 0, b.speedErr()
	}

	// Arm a stall bound: this read may not block longer than window without
	// delivering data. time.AfterFunc closes the underlying body from another
	// goroutine, which unblocks a stalled Read.
	timer := time.AfterFunc(b.window, b.trip)
	start := time.Now()
	n, err := b.rc.Read(p)
	timer.Stop()

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
