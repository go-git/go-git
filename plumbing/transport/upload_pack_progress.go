package transport

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// progressWriter emits rate-limited sideband progress lines to the
// underlying writer (typically the band-2 channel of a sideband muxer).
// It is safe for concurrent Update calls. Close must be called before
// writing any trailing data on the same muxer so no progress frame is
// emitted after the pack trailer.
//
// A nil underlying writer turns the writer into a no-op; this lets
// callers construct one unconditionally and pass nil when sideband was
// not negotiated.
type progressWriter struct {
	w        io.Writer
	interval time.Duration

	mu      sync.Mutex
	pending string
	closed  bool

	stop chan struct{}
	done chan struct{}
}

func newProgressWriter(w io.Writer, interval time.Duration) *progressWriter {
	p := &progressWriter{
		w:        w,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	if w == nil {
		close(p.done)
		return p
	}
	go p.run()
	return p
}

// Update sets the latest pending message. The line is rewritten with
// `\r` until the phase is closed via Flush.
func (p *progressWriter) Update(format string, args ...interface{}) {
	if p == nil || p.w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.pending = fmt.Sprintf(format, args...)
}

// Flush emits the pending message terminated with a newline, marking the
// phase complete (e.g. "Counting objects: 1234, done.\n"). Subsequent
// Updates after Flush start a new phase.
func (p *progressWriter) Flush(format string, args ...interface{}) {
	if p == nil || p.w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	line := fmt.Sprintf(format, args...) + "\n"
	_, _ = p.w.Write([]byte(line))
	p.pending = ""
}

// Close stops the rate-limit goroutine. Safe to call multiple times.
func (p *progressWriter) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	already := p.closed
	p.closed = true
	p.mu.Unlock()
	if already || p.w == nil {
		return
	}
	close(p.stop)
	<-p.done
}

func (p *progressWriter) run() {
	defer close(p.done)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			p.emit()
		case <-p.stop:
			p.emit()
			return
		}
	}
}

func (p *progressWriter) emit() {
	p.mu.Lock()
	if p.pending == "" {
		p.mu.Unlock()
		return
	}
	line := p.pending + "\r"
	p.pending = ""
	p.mu.Unlock()
	_, _ = p.w.Write([]byte(line))
}
