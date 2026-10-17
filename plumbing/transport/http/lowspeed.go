package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// LowSpeedGuard aborts an HTTP transfer when its sampled upload and download
// rate stays below Limit for Time. Both fields must be positive to enable it.
//
// Like Git's http.lowSpeedLimit/http.lowSpeedTime, this checks a rolling rate,
// not a deadline for each read. It samples combined body progress once per
// second over up to five intervals. Time values below one second also shorten
// the sampling interval; Git only accepts whole seconds.
//
// Unlike libcurl, net/http exposes body progress rather than socket progress.
// Buffering can therefore change the measured rate. Elapsed time includes
// connection setup and pauses between caller reads; there is no pause API.
// A custom RoundTripper must honor request cancellation.
type LowSpeedGuard struct {
	// Limit is the minimum combined transfer rate in bytes per second.
	// A zero or negative value disables the guard.
	Limit int64
	// Time is how long the sampled rate may remain below Limit.
	// A zero or negative value disables the guard.
	Time time.Duration
}

func (g *LowSpeedGuard) valid() bool {
	return g != nil && g.Limit > 0 && g.Time > 0
}

type lowSpeedError struct {
	guard LowSpeedGuard
}

func (e *lowSpeedError) Error() string {
	return fmt.Sprintf("http transport: transfer speed below %d bytes/sec for %s", e.guard.Limit, e.guard.Time)
}

func (*lowSpeedError) Timeout() bool { return true }

func (*lowSpeedError) Unwrap() error { return context.DeadlineExceeded }

type lowSpeedSample struct {
	at    time.Time
	bytes int64
}

// Six samples span five intervals, as in libcurl's progress.c.
type lowSpeedWindow struct {
	samples [6]lowSpeedSample
	next    int
	count   int
	slowAt  time.Time
}

func (w *lowSpeedWindow) expired(now time.Time, total int64, guard LowSpeedGuard) bool {
	w.samples[w.next] = lowSpeedSample{at: now, bytes: total}
	w.next = (w.next + 1) % len(w.samples)
	w.count = min(w.count+1, len(w.samples))

	oldest := w.samples[0]
	if w.count == len(w.samples) {
		oldest = w.samples[w.next]
	}
	elapsed := now.Sub(oldest.at)
	if elapsed > 0 && float64(total-oldest.bytes)/elapsed.Seconds() >= float64(guard.Limit) {
		w.slowAt = time.Time{}
		return false
	}
	if w.slowAt.IsZero() {
		w.slowAt = now
	}
	return now.Sub(w.slowAt) >= guard.Time
}

type lowSpeedMonitor struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	total  atomic.Int64
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

func (m *lowSpeedMonitor) watch(start time.Time, guard LowSpeedGuard) {
	defer close(m.done)
	ticker := time.NewTicker(min(time.Second, guard.Time))
	defer ticker.Stop()

	var window lowSpeedWindow
	window.expired(start, 0, guard)
	for {
		select {
		case <-m.stop:
			return
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if window.expired(time.Now(), m.total.Load(), guard) {
				m.cancel(&lowSpeedError{guard: guard})
				return
			}
		}
	}
}

func (m *lowSpeedMonitor) finish() {
	m.once.Do(func() { close(m.stop) })
	<-m.done
}

type lowSpeedTransport struct {
	http.RoundTripper
	guard LowSpeedGuard
}

func (t *lowSpeedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	m := &lowSpeedMonitor{
		ctx: ctx, cancel: cancel,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go m.watch(time.Now(), t.guard)

	req = req.Clone(ctx)
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &lowSpeedReader{ReadCloser: req.Body, total: &m.total}
	}
	if req.GetBody != nil {
		getBody := req.GetBody
		req.GetBody = func() (io.ReadCloser, error) {
			body, err := getBody()
			if err != nil {
				return nil, err
			}
			return &lowSpeedReader{ReadCloser: body, total: &m.total}, nil
		}
	}

	resp, err := t.RoundTripper.RoundTrip(req)
	if err != nil || resp == nil {
		m.finish()
		if cause := context.Cause(ctx); cause != nil {
			err = cause
		}
		cancel(nil)
		return resp, err
	}
	if resp.Request == nil {
		resp.Request = req
	} else {
		resp.Request = resp.Request.WithContext(ctx)
	}
	if resp.Body == nil || resp.Body == http.NoBody {
		m.finish()
		cancel(nil)
	} else {
		resp.Body = &lowSpeedBody{
			lowSpeedReader: lowSpeedReader{ReadCloser: resp.Body, total: &m.total},
			monitor:        m,
		}
	}
	return resp, nil
}

func (t *lowSpeedTransport) CloseIdleConnections() {
	if closer, ok := t.RoundTripper.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type lowSpeedReader struct {
	io.ReadCloser
	total *atomic.Int64
}

func (r *lowSpeedReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.total.Add(int64(n))
	return n, err
}

type lowSpeedBody struct {
	lowSpeedReader
	monitor  *lowSpeedMonitor
	once     sync.Once
	closeErr error
}

func (b *lowSpeedBody) Read(p []byte) (int, error) {
	n, err := b.lowSpeedReader.Read(p)
	if err != nil {
		b.monitor.finish()
		if cause := context.Cause(b.monitor.ctx); cause != nil {
			err = cause
		}
	}
	return n, err
}

func (b *lowSpeedBody) Close() error {
	b.once.Do(func() {
		b.monitor.finish()
		b.monitor.cancel(nil)
		b.closeErr = b.ReadCloser.Close()
	})
	return b.closeErr
}
