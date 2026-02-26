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

	select {
	case ret := <-done:
		if ret.n != 0 {
			t.Error("ret.n should be 0", ret.n)
		}
		if !errors.Is(ret.err, context.Canceled) {
			t.Error("ret.err should be ctx error", ret.err)
		}
	case <-time.After(20 * time.Millisecond):
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

	select {
	case ret := <-done:
		if ret.n != 0 {
			t.Error("ret.n should be 0", ret.n)
		}
		if !errors.Is(ret.err, context.Canceled) {
			t.Error("ret.err should be ctx error", ret.err)
		}
	case <-time.After(20 * time.Millisecond):
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

func BenchmarkContextReader(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()
	r := bytes.NewReader(data)
	ctxr := NewContextReader(ctx, r)

	for b.Loop() {
		r.Reset(data)

		n, _ := ctxr.Read(data)
		b.SetBytes(int64(n))
	}
}

func BenchmarkContextWriter(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()
	ctxw := NewContextWriter(ctx, io.Discard)

	for b.Loop() {
		n, _ := ctxw.Write(data)
		b.SetBytes(int64(n))
	}
}
