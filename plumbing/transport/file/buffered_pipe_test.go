package file

import (
	"io"
	"testing"
)

func TestBufferedPipe_BasicReadWrite(t *testing.T) {
	pr, pw := newBufferedPipe()

	// Write some data
	data := []byte("hello world")
	n, err := pw.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Fatalf("Write returned %d, want %d", n, len(data))
	}

	// Read it back
	buf := make([]byte, 100)
	n, err = pr.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != "hello world" {
		t.Fatalf("Read returned %q, want %q", string(buf[:n]), "hello world")
	}
}

func TestBufferedPipe_WriteBeforeRead(t *testing.T) {
	pr, pw := newBufferedPipe()

	// Write multiple times before reading (this would deadlock with io.Pipe)
	for i := 0; i < 100; i++ {
		_, err := pw.Write([]byte("test data "))
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}

	// Now read all the data
	buf := make([]byte, 2000)
	total := 0
	for total < 1000 {
		n, err := pr.Read(buf[total:])
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		total += n
	}
}

func TestBufferedPipe_CloseWriter(t *testing.T) {
	pr, pw := newBufferedPipe()

	pw.Write([]byte("data"))
	pw.Close()

	// Should still be able to read buffered data
	buf := make([]byte, 100)
	n, err := pr.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != "data" {
		t.Fatalf("Read returned %q, want %q", string(buf[:n]), "data")
	}

	// Next read should return EOF
	_, err = pr.Read(buf)
	if err != io.EOF {
		t.Fatalf("Read after close returned %v, want EOF", err)
	}
}

func TestBufferedPipe_CloseReader(t *testing.T) {
	pr, pw := newBufferedPipe()

	pr.Close()

	// Write should fail
	_, err := pw.Write([]byte("data"))
	if err != io.ErrClosedPipe {
		t.Fatalf("Write after reader close returned %v, want ErrClosedPipe", err)
	}
}

func TestBufferedPipe_BufferFull(t *testing.T) {
	pr, pw := newBufferedPipe()
	_ = pr // silence unused warning

	// Write more than maxBufferSize
	bigData := make([]byte, maxBufferSize+1)
	_, err := pw.Write(bigData)
	if err != ErrBufferFull {
		t.Fatalf("Write of %d bytes returned %v, want ErrBufferFull", len(bigData), err)
	}
}

func TestBufferedPipe_BufferFullIncremental(t *testing.T) {
	pr, pw := newBufferedPipe()
	_ = pr // silence unused warning

	// Fill the buffer incrementally
	chunk := make([]byte, 1024*1024) // 1MB chunks
	written := 0
	for {
		_, err := pw.Write(chunk)
		if err == ErrBufferFull {
			break
		}
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		written += len(chunk)
		if written > maxBufferSize {
			t.Fatal("Should have hit buffer limit")
		}
	}

	// Should have written close to maxBufferSize
	if written < maxBufferSize-len(chunk) {
		t.Fatalf("Only wrote %d bytes, expected close to %d", written, maxBufferSize)
	}
}
