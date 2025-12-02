package ioutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestReader(t *testing.T) {
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

func BenchmarkContextReader(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()

	b.ResetTimer()

	for range b.N {
		r := bytes.NewReader(data)

		ctxr := NewContextReader(ctx, r)
		io.Copy(io.Discard, ctxr)
	}
}

func BenchmarkContextWriter(b *testing.B) {
	data := bytes.Repeat([]byte{'0'}, 10000)

	ctx := context.Background()

	b.ResetTimer()

	for range b.N {
		ctxw := NewContextWriter(ctx, io.Discard)
		ctxw.Write(data)
	}
}
