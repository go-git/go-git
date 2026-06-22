package ioutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReader(t *testing.T) {
	t.Parallel()
	buf := []byte("abcdef")
	buf2 := make([]byte, 3)
	r := NewContextReader(context.Background(), bytes.NewReader(buf))

	// read first half
	n, err := r.Read(buf2)
	if n != 3 {
		t.Error("n should be 3")
	}
	if err != nil {
		t.Error("should have no error")
	}
	if string(buf2) != string(buf[:3]) {
		t.Error("incorrect contents")
	}

	// read second half
	n, err = r.Read(buf2)
	if n != 3 {
		t.Error("n should be 3")
	}
	if err != nil {
		t.Error("should have no error")
	}
	if string(buf2) != string(buf[3:6]) {
		t.Error("incorrect contents")
	}

	// read more.
	n, err = r.Read(buf2)
	if n != 0 {
		t.Error("n should be 0", n)
	}
	if err != io.EOF {
		t.Error("should be EOF", err)
	}
}

func TestWriter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := NewContextWriter(context.Background(), &buf)

	// write three
	n, err := w.Write([]byte("abc"))
	if n != 3 {
		t.Error("n should be 3")
	}
	if err != nil {
		t.Error("should have no error")
	}
	if buf.String() != "abc" {
		t.Error("incorrect contents")
	}

	// write three more
	n, err = w.Write([]byte("def"))
	if n != 3 {
		t.Error("n should be 3")
	}
	if err != nil {
		t.Error("should have no error")
	}
	if buf.String() != "abcdef" {
		t.Error("incorrect contents")
	}
}

func TestReaderCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	piper, pipew := io.Pipe()
	r := NewContextReader(ctx, piper)

	buf := make([]byte, 10)
	done := make(chan ioret)

	go func() {
		n, err := r.Read(buf)
		done <- ioret{err, n}
	}()

	pipew.Write([]byte("abcdefghij"))

	select {
	case ret := <-done:
		if ret.n != 10 {
			t.Error("ret.n should be 10", ret.n)
		}
		if ret.err != nil {
			t.Error("ret.err should be nil", ret.err)
		}
		if string(buf) != "abcdefghij" {
			t.Error("read contents differ")
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatal("failed to read")
	}

	go func() {
		n, err := r.Read(buf)
		done <- ioret{err, n}
	}()

	cancel()

	select {
	case ret := <-done:
		if ret.n != 0 {
			t.Error("ret.n should be 0", ret.n)
		}
		if ret.err == nil {
			t.Error("ret.err should be ctx error", ret.err)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatal("failed to stop reading after cancel")
	}
}

func TestWriterCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	piper, pipew := io.Pipe()
	w := NewContextWriter(ctx, pipew)

	buf := make([]byte, 10)
	done := make(chan ioret)

	go func() {
		n, err := w.Write([]byte("abcdefghij"))
		done <- ioret{err, n}
	}()

	piper.Read(buf)

	select {
	case ret := <-done:
		if ret.n != 10 {
			t.Error("ret.n should be 10", ret.n)
		}
		if ret.err != nil {
			t.Error("ret.err should be nil", ret.err)
		}
		if string(buf) != "abcdefghij" {
			t.Error("write contents differ")
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatal("failed to write")
	}

	go func() {
		n, err := w.Write([]byte("abcdefghij"))
		done <- ioret{err, n}
	}()

	cancel()

	// Write waits for the in-flight underlying write to complete before
	// returning on context cancel — otherwise the inner goroutine would
	// still be touching the underlying writer after Write returns.
	// Unblock pipew by reading from the other end.
	go func() { _, _ = piper.Read(buf) }()

	select {
	case ret := <-done:
		if !errors.Is(ret.err, context.Canceled) {
			t.Error("ret.err should be ctx error", ret.err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed to stop writing after cancel")
	}
}

func TestReadPostCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	piper, _ := io.Pipe()
	r := NewContextReader(ctx, piper)

	buf := make([]byte, 10)
	done := make(chan ioret)

	go func() {
		n, err := r.Read(buf)
		done <- ioret{err, n}
	}()

	cancel()

	select {
	case ret := <-done:
		if ret.n != 0 {
			t.Error("ret.n should be 0", ret.n)
		}
		if ret.err == nil {
			t.Error("ret.err should be ctx error", ret.err)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatal("failed to stop reading after cancel")
	}
}

func TestWritePostCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	piper, pipew := io.Pipe()
	w := NewContextWriter(ctx, pipew)

	buf := []byte("abcdefghij")
	buf2 := make([]byte, 10)
	done := make(chan ioret)

	go func() {
		n, err := w.Write(buf)
		done <- ioret{err, n}
	}()

	piper.Read(buf2)

	select {
	case ret := <-done:
		if ret.n != 10 {
			t.Error("ret.n should be 10", ret.n)
		}
		if ret.err != nil {
			t.Error("ret.err should be nil", ret.err)
		}
		if string(buf2) != "abcdefghij" {
			t.Error("write contents differ")
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatal("failed to write")
	}

	go func() {
		n, err := w.Write(buf)
		done <- ioret{err, n}
	}()

	cancel()

	// Write waits for the in-flight underlying write to complete before
	// returning on context cancel. Drain pipew so the inner goroutine can
	// finish.
	go func() { _, _ = piper.Read(buf2) }()

	select {
	case ret := <-done:
		if !errors.Is(ret.err, context.Canceled) {
			t.Error("ret.err should be ctx error", ret.err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed to stop writing after cancel")
	}
}

func TestReadUnderlyingPanics(t *testing.T) {
	t.Parallel()

	r := NewContextReader(context.Background(), nil)

	done := make(chan struct{}, 1)

	go func() {
		n, err := r.Read([]byte{})
		assert.Error(t, err)
		assert.Equal(t, 0, n)

		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("test timed out")
	}
}

func TestWriteUnderlyingPanics(t *testing.T) {
	t.Parallel()

	r := NewContextWriter(context.Background(), nil)

	done := make(chan struct{}, 1)

	go func() {
		n, err := r.Write([]byte{'a'})
		assert.Error(t, err)
		assert.Equal(t, 0, n)

		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("test timed out")
	}
}

// blockingWriter blocks each Write call until release is signalled.
type blockingWriter struct {
	release chan struct{}
	started chan struct{}
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	return len(p), nil
}

func TestWriterCancelWaitsForInFlightWrite(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bw := &blockingWriter{
		release: make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	w := NewContextWriter(ctx, bw)

	done := make(chan struct{})
	go func() {
		_, _ = w.Write([]byte("hello"))
		close(done)
	}()

	<-bw.started
	cancel()

	select {
	case <-done:
		t.Fatal("Write returned before the in-flight underlying write completed")
	case <-time.After(20 * time.Millisecond):
	}

	close(bw.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Write did not return after underlying write completed")
	}
}

// panicOnReleaseWriter blocks each Write until release is signalled, then
// panics — modelling an underlying writer that fails while the context is
// already cancelled.
type panicOnReleaseWriter struct {
	release chan struct{}
	started chan struct{}
}

func (p *panicOnReleaseWriter) Write(_ []byte) (int, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	<-p.release
	panic("underlying write failed")
}

// On cancel, Write drains the in-flight result before returning. If that
// in-flight write panics, the recover path must still deliver a result so the
// drain cannot block forever.
func TestWriterCancelPanicDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pw := &panicOnReleaseWriter{
		release: make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	w := NewContextWriter(ctx, pw)

	done := make(chan struct{})
	go func() {
		_, _ = w.Write([]byte("hello"))
		close(done)
	}()

	<-pw.started
	cancel()

	select {
	case <-done:
		t.Fatal("Write returned before the in-flight underlying write completed")
	case <-time.After(20 * time.Millisecond):
	}

	close(pw.release) // the in-flight write now panics

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Write deadlocked draining a panicking in-flight write on cancel")
	}
}

// An already-cancelled context must make Write return ctx.Err() promptly,
// without hanging, when the underlying write completes on its own.
func TestWriterPreCancelledContextReturnsPromptly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	w := NewContextWriter(ctx, &buf)

	done := make(chan ioret, 1)
	go func() {
		n, err := w.Write([]byte("hello"))
		done <- ioret{err, n}
	}()

	select {
	case ret := <-done:
		if !errors.Is(ret.err, context.Canceled) {
			t.Errorf("err should be context.Canceled, got %v", ret.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Write did not return for an already-cancelled context")
	}
}

// multiChunkWriter lets the first chunk through, then blocks every later chunk
// on release, signalling once the second chunk has started.
type multiChunkWriter struct {
	secondStarted chan struct{}
	release       chan struct{}
	calls         int
}

func (w *multiChunkWriter) Write(b []byte) (int, error) {
	w.calls++
	if w.calls >= 2 {
		if w.calls == 2 {
			close(w.secondStarted)
		}
		<-w.release
	}
	return len(b), nil
}

// A buffer larger than the 32KiB pool slice forces several underlying writes.
// Cancelling while a later chunk is in flight must not deadlock: Write waits
// for that chunk, then returns ctx.Err() once it is unblocked.
func TestWriterCancelMultiChunkDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mw := &multiChunkWriter{
		secondStarted: make(chan struct{}),
		release:       make(chan struct{}),
	}
	w := NewContextWriter(ctx, mw)

	done := make(chan ioret, 1)
	go func() {
		n, err := w.Write(bytes.Repeat([]byte("x"), 80*1024))
		done <- ioret{err, n}
	}()

	<-mw.secondStarted // first chunk done, second chunk now blocked
	cancel()

	select {
	case <-done:
		t.Fatal("Write returned before the in-flight chunk completed")
	case <-time.After(20 * time.Millisecond):
	}

	close(mw.release)

	select {
	case ret := <-done:
		if !errors.Is(ret.err, context.Canceled) {
			t.Errorf("err should be context.Canceled, got %v", ret.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Write deadlocked on cancel during a multi-chunk write")
	}
}

// Racing cancellation against a completing underlying write must never
// deadlock, regardless of which side wins. Run under -race to also exercise
// the synchronisation between the inner goroutine and Write.
func TestWriterCancelRaceNoDeadlock(t *testing.T) {
	t.Parallel()

	for range 200 {
		ctx, cancel := context.WithCancel(context.Background())
		w := NewContextWriter(ctx, io.Discard)

		done := make(chan struct{})
		go func() {
			_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
			close(done)
		}()
		go cancel()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Write deadlocked under concurrent cancellation")
		}
	}
}

func BenchmarkContextReader(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()

	for b.Loop() {
		r := bytes.NewReader(data)

		ctxr := NewContextReader(ctx, r)

		n, _ := ctxr.Read(data)
		b.SetBytes(int64(n))
	}
}

func BenchmarkContextWriter(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()

	for b.Loop() {
		ctxw := NewContextWriter(ctx, io.Discard)

		n, _ := ctxw.Write(data)
		b.SetBytes(int64(n))
	}
}
