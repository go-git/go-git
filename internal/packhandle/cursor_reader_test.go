package packhandle

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"sync"
	"testing"
	"time"
)

func TestCursorReader_ReadAdvancesOffset(t *testing.T) {
	t.Parallel()
	data := []byte("PACK0123456789")
	sf := newSharedFile(func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(data)}, nil
	}, 1*time.Hour)
	defer sf.Close()

	c, err := newCursorReader(sf, int64(len(data)))
	if err != nil {
		t.Fatalf("newCursorReader: %v", err)
	}
	defer c.Close()

	buf := make([]byte, 4)
	n, err := c.Read(buf)
	if err != nil || n != 4 || string(buf) != "PACK" {
		t.Fatalf("Read 1: n=%d err=%v buf=%q", n, err, buf)
	}
	n, err = c.Read(buf)
	if err != nil || n != 4 || string(buf) != "0123" {
		t.Fatalf("Read 2: n=%d err=%v buf=%q", n, err, buf)
	}
}

func TestCursorReader_ReadAtIndependentOfOffset(t *testing.T) {
	t.Parallel()
	data := []byte("PACK0123456789")
	sf := newSharedFile(func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(data)}, nil
	}, 1*time.Hour)
	defer sf.Close()

	c, err := newCursorReader(sf, int64(len(data)))
	if err != nil {
		t.Fatalf("newCursorReader: %v", err)
	}
	defer c.Close()

	buf4 := make([]byte, 4)
	if _, err := c.Read(buf4); err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Sequential read advanced cursor offset to 4.

	buf2 := make([]byte, 2)
	if _, err := c.ReadAt(buf2, 10); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if string(buf2) != "67" {
		t.Fatalf("ReadAt: got %q, want \"67\"", buf2)
	}

	// Next sequential Read must continue from cursor offset 4 (not affected by ReadAt).
	if _, err := c.Read(buf4); err != nil {
		t.Fatalf("Read after ReadAt: %v", err)
	}
	if string(buf4) != "0123" {
		t.Fatalf("Read after ReadAt: got %q, want \"0123\"", buf4)
	}
}

func TestCursorReader_SeekEndUsesSize(t *testing.T) {
	t.Parallel()
	data := []byte("PACK0123456789")
	sf := newSharedFile(func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(data)}, nil
	}, 1*time.Hour)
	defer sf.Close()

	c, err := newCursorReader(sf, int64(len(data)))
	if err != nil {
		t.Fatalf("newCursorReader: %v", err)
	}
	defer c.Close()

	pos, err := c.Seek(-4, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek SeekEnd: %v", err)
	}
	if pos != int64(len(data)-4) {
		t.Fatalf("Seek pos = %d, want %d", pos, len(data)-4)
	}

	buf := make([]byte, 4)
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("Read after Seek: %v", err)
	}
	if string(buf) != "6789" {
		t.Fatalf("Read after Seek: got %q, want \"6789\"", buf)
	}
}

func TestCursorReader_CloseIsIdempotent(t *testing.T) {
	t.Parallel()
	sf := newSharedFile(func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader([]byte("PACK"))}, nil
	}, 1*time.Hour)
	defer sf.Close()

	c, err := newCursorReader(sf, 4)
	if err != nil {
		t.Fatalf("newCursorReader: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}

func TestCursorReader_ReadPastEnd(t *testing.T) {
	t.Parallel()
	data := []byte("PACK")
	sf := newSharedFile(func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(data)}, nil
	}, 1*time.Hour)
	defer sf.Close()

	c, _ := newCursorReader(sf, int64(len(data)))
	defer c.Close()

	buf := make([]byte, 100)
	n, err := c.Read(buf)
	if n != 4 {
		t.Fatalf("Read partial: n=%d, want 4", n)
	}
	// First Read may or may not return io.EOF — both are valid per io.Reader contract;
	// what matters is the byte count.
	_ = err
	n, err = c.Read(buf)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("Read past end: n=%d err=%v, want 0/io.EOF", n, err)
	}
}

func TestCursorReader_AfterCloseReturnsErrClosed(t *testing.T) {
	t.Parallel()
	sf := newSharedFile(func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader([]byte("PACK"))}, nil
	}, 1*time.Hour)
	defer sf.Close()

	c, err := newCursorReader(sf, 4)
	if err != nil {
		t.Fatalf("newCursorReader: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	buf := make([]byte, 4)
	if _, err := c.Read(buf); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Read after Close: err = %v, want fs.ErrClosed", err)
	}
	if _, err := c.ReadAt(buf, 0); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("ReadAt after Close: err = %v, want fs.ErrClosed", err)
	}
	if _, err := c.Seek(0, io.SeekStart); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("Seek after Close: err = %v, want fs.ErrClosed", err)
	}
}

func TestCursorReader_ConcurrentCloseAndRead(t *testing.T) {
	t.Parallel()
	// Exercises Close racing with in-flight Read. The closed-flag
	// transition must serialize cleanly: any Read that passed the
	// closed check before Close sets the flag must complete without
	// panic; any Read that arrives after Close must see fs.ErrClosed.
	data := []byte("PACK0123456789")
	sf := newSharedFile(func() (ReadAtCloser, error) {
		return &memCloser{Reader: bytes.NewReader(data)}, nil
	}, 1*time.Hour)
	defer sf.Close()

	const goroutines = 16
	const iterations = 100

	for range iterations {
		c, err := newCursorReader(sf, int64(len(data)))
		if err != nil {
			t.Fatalf("newCursorReader: %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(goroutines)
		for range goroutines {
			go func() {
				defer wg.Done()
				buf := make([]byte, 4)
				_, _ = c.ReadAt(buf, 0)
			}()
		}
		_ = c.Close()
		wg.Wait()
	}
}
