package pktline_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func TestProgressWriter_NilOptsDefaults(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewProgressWriter(&buf, nil)
	if _, err := w.Write([]byte("Counting objects\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "remote: Counting objects\n" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestProgressWriter_EmptyPrefix(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewProgressWriter(&buf, &pktline.ProgressOptions{})
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "hello\n" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestProgressWriter_LineBufferingAcrossWrites(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewProgressWriter(&buf, &pktline.ProgressOptions{Prefix: "remote: "})
	for _, chunk := range []string{"Counti", "ng obj", "ects\nResolving "} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if buf.String() != "remote: Counting objects\n" {
		t.Fatalf("after partial line: got %q", buf.String())
	}
	if _, err := w.Write([]byte("deltas: 100%\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "remote: Counting objects\nremote: Resolving deltas: 100%\n" {
		t.Fatalf("after second line: got %q", buf.String())
	}
}

func TestProgressWriter_CR_TTYAppendsEraseLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewProgressWriter(&buf, &pktline.ProgressOptions{
		Prefix: "remote: ",
		TTY:    true,
	})
	if _, err := w.Write([]byte("Resolving: 50%\rResolving: 100%\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := "remote: Resolving: 50%\r\x1b[K" + "remote: Resolving: 100%\r\x1b[K"
	if buf.String() != want {
		t.Fatalf("got %q\nwant %q", buf.String(), want)
	}
}

func TestProgressWriter_CR_NonTTYRewritesToNewline(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewProgressWriter(&buf, &pktline.ProgressOptions{
		Prefix: "remote: ",
		TTY:    false,
	})
	if _, err := w.Write([]byte("Resolving: 50%\rResolving: 100%\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := "remote: Resolving: 50%\nremote: Resolving: 100%\n"
	if buf.String() != want {
		t.Fatalf("got %q\nwant %q", buf.String(), want)
	}
}

func TestProgressWriter_CloseFlushesPending(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewProgressWriter(&buf, nil)
	if _, err := w.Write([]byte("trailing")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("partial line leaked before Close: %q", buf.String())
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.String() != "remote: trailing" {
		t.Fatalf("after Close: %q", buf.String())
	}
}

func TestProgressWriter_NilWriter(t *testing.T) {
	t.Parallel()
	w := pktline.NewProgressWriter(nil, nil)
	if _, err := w.Write([]byte("ignored\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// fakeTTYWriter satisfies the Fd() interface used by the TTY auto-detect
// path, returning a file descriptor that is not a terminal.
type fakeTTYWriter struct {
	io.Writer
	fd uintptr
}

func (f *fakeTTYWriter) Fd() uintptr { return f.fd }

func TestProgressWriter_AutoDetectTTY_NonTerminal(t *testing.T) {
	t.Parallel()
	// A pipe's read end is not a terminal: the auto-detect path should
	// resolve TTY to false, which means \r is rewritten to \n.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	var captured bytes.Buffer
	wrapped := &fakeTTYWriter{Writer: &captured, fd: pw.Fd()}
	w := pktline.NewProgressWriter(wrapped, nil)
	if _, err := w.Write([]byte("Progress\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if captured.String() != "remote: Progress\n" {
		t.Fatalf("got %q", captured.String())
	}
}

func TestProgressWriter_NoFdInterfaceDefaultsNonTTY(t *testing.T) {
	t.Parallel()
	// *bytes.Buffer has no Fd() method, so TTY auto-detect resolves false.
	var buf bytes.Buffer
	w := pktline.NewProgressWriter(&buf, nil)
	if _, err := w.Write([]byte("Progress\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "remote: Progress\n" {
		t.Fatalf("got %q", buf.String())
	}
}
