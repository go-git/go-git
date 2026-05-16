package pktline

import (
	"bytes"
	"io"

	"golang.org/x/term"
)

// DefaultProgressPrefix is prepended to each progress line emitted by a
// ProgressWriter constructed with nil options, matching the prefix
// applied by canonical Git in recv_sideband.
const DefaultProgressPrefix = "remote: "

// ansiEraseLine clears from the cursor to the end of the line. On a
// terminal it is written at the start of every progress line (before the
// prefix) so that a shorter line overwriting a longer one does not leave
// stale trailing characters. This mirrors canonical Git, whose terminal
// progress prefix is "\x1b[K" + "remote: " (ANSI_PREFIX DISPLAY_PREFIX in
// sideband.c).
var ansiEraseLine = []byte("\x1b[K")

// ProgressOptions configures a [ProgressWriter].
//
// The zero value is a usable configuration that emits raw line-buffered
// output without a prefix and without terminal-aware carriage-return
// handling. To get Git-like defaults instead (a "remote: " prefix and
// TTY auto-detection based on the destination writer), pass nil to
// [NewProgressWriter].
type ProgressOptions struct {
	// Prefix is prepended to every line written to the destination.
	// An empty string disables the prefix.
	Prefix string
	// TTY enables terminal-aware carriage-return handling. When true,
	// each line is prefixed with an ANSI erase-to-end-of-line sequence
	// (matching Git's terminal output) so a shorter line overwriting a
	// longer one does not leave stale characters, and \r terminators are
	// preserved so progress updates overwrite in place. When false,
	// carriage returns are rewritten to newlines so non-terminal sinks
	// (log files, pipes) get the full progress history rather than a
	// single overwriting line.
	TTY bool
}

// ProgressWriter buffers raw sideband progress bytes per line and
// renders them to an underlying [io.Writer] with an optional prefix and
// terminal-aware carriage-return handling.
//
// ProgressWriter is intended to wrap the destination passed as the
// progress argument to [NewSidebandScanner] or [NewSidebandReader],
// turning the raw band-2 byte stream into human-facing output.
//
// A ProgressWriter is not safe for concurrent use.
type ProgressWriter struct {
	w       io.Writer
	prefix  []byte
	tty     bool
	pending []byte // partial line not yet terminated by \r or \n
}

// NewProgressWriter wraps w with sideband-progress presentation.
//
// If opts is nil, Git-like defaults are applied: Prefix is
// [DefaultProgressPrefix] ("remote: ") and TTY is auto-detected from w
// (true when w is backed by a file descriptor that is a terminal). To
// opt out of either default, pass a non-nil *ProgressOptions and set the
// fields explicitly.
func NewProgressWriter(w io.Writer, opts *ProgressOptions) *ProgressWriter {
	if w == nil {
		w = io.Discard
	}
	var resolved ProgressOptions
	if opts == nil {
		resolved.Prefix = DefaultProgressPrefix
		resolved.TTY = isWriterTerminal(w)
	} else {
		resolved = *opts
	}
	prefix := []byte(resolved.Prefix)
	if resolved.TTY {
		// On a terminal, erase to end of line before drawing each line,
		// matching Git's ANSI_PREFIX DISPLAY_PREFIX ("\x1b[K" + "remote: ").
		prefix = append(append([]byte{}, ansiEraseLine...), prefix...)
	}
	return &ProgressWriter{
		w:      w,
		prefix: prefix,
		tty:    resolved.TTY,
	}
}

// Write appends p to the internal line buffer, then flushes every
// complete line (terminated by \r or \n) to the underlying writer.
// Partial trailing input is retained until the next Write or Close.
//
// Write always returns len(p) and a nil error; underlying write errors
// are dropped because progress output is best-effort.
func (p *ProgressWriter) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p.pending = append(p.pending, b...)
	for {
		i := bytes.IndexAny(p.pending, "\r\n")
		if i < 0 {
			return len(b), nil
		}
		p.emit(p.pending[:i+1])
		p.pending = p.pending[i+1:]
	}
}

// Close flushes any buffered partial line to the underlying writer.
// Close does not close the underlying writer.
func (p *ProgressWriter) Close() error {
	if len(p.pending) == 0 {
		return nil
	}
	p.emit(p.pending)
	p.pending = p.pending[:0]
	return nil
}

// emit renders one logical line (with or without its trailing
// terminator) to the underlying writer, applying Prefix and TTY rules.
// On a terminal the prefix already carries the erase-to-end-of-line
// sequence and \r terminators are preserved so updates overwrite in
// place; otherwise \r terminators are rewritten to \n so non-terminal
// sinks retain the full progress history.
func (p *ProgressWriter) emit(line []byte) {
	out := make([]byte, 0, len(p.prefix)+len(line))
	if len(p.prefix) > 0 {
		out = append(out, p.prefix...)
	}
	if n := len(line); !p.tty && n > 0 && line[n-1] == '\r' {
		out = append(out, line[:n-1]...)
		out = append(out, '\n')
	} else {
		out = append(out, line...)
	}
	_, _ = p.w.Write(out)
}

// isWriterTerminal reports whether w is backed by a file descriptor
// that refers to a terminal. It returns false for writers that do not
// expose an Fd() method (e.g. bytes.Buffer, io.MultiWriter).
func isWriterTerminal(w io.Writer) bool {
	type fdWriter interface {
		Fd() uintptr
	}
	if f, ok := w.(fdWriter); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}
