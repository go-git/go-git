package transport

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProgressWriter_RateLimits(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{w: &buf, mu: &mu}

	p := newProgressWriter(w, 50*time.Millisecond)

	// Six rapid updates within 25 ms should collapse to at most a couple of
	// emitted frames after the timer fires.
	for i := 0; i < 6; i++ {
		p.Update("Counting objects: %d", i)
		time.Sleep(4 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond) // allow ticker fire
	p.Close()

	mu.Lock()
	defer mu.Unlock()
	got := buf.String()
	if got == "" {
		t.Fatal("expected at least one progress frame")
	}
	if !strings.Contains(got, "Counting objects: 5") {
		t.Fatalf("expected latest update emitted, got %q", got)
	}
}

func TestProgressWriter_NilWriterIsNoop(t *testing.T) {
	t.Parallel()
	p := newProgressWriter(nil, 50*time.Millisecond)
	p.Update("hello") // must not panic
	p.Close()
}

func TestProgressWriter_FlushEmitsLineAndClearsPending(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var mu sync.Mutex
	w := &syncWriter{w: &buf, mu: &mu}

	p := newProgressWriter(w, 25*time.Millisecond)

	p.Update("Counting objects: 1")
	p.Flush("Counting objects: 1, done.")

	// Allow the ticker an opportunity to fire after Flush; it should
	// observe empty pending and emit nothing more.
	time.Sleep(80 * time.Millisecond)
	p.Close()

	mu.Lock()
	defer mu.Unlock()
	got := buf.String()
	if !strings.Contains(got, "Counting objects: 1, done.\n") {
		t.Fatalf("expected flush newline-terminated line, got %q", got)
	}
	// After Flush, any further emissions would be the rolling \r form.
	// We tolerate one prior \r frame from the pre-Flush Update window
	// (the ticker may or may not have fired in time), but not anything
	// after Flush. A simple proxy: the buffer ends with \n.
	if got[len(got)-1] != '\n' {
		t.Fatalf("expected buffer to end with newline (no post-Flush emit), got %q", got)
	}
}

type syncWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
