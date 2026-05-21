package pktline

import (
	"errors"
	"io"
)

// Writer writes pkt-lines to an underlying io.Writer.
//
// In plain mode (constructed with [NewWriter]), Write emits raw
// pkt-lines: payloads larger than the chunk limit are split across
// multiple pkt-lines.
//
// In sideband mode (constructed with [NewSidebandWriter]), Write emits
// [BandData] pkt-lines, WriteProgress emits [BandProgress] pkt-lines,
// and each on-wire pkt-line is bounded by the maxSize parameter. The only
// difference between plain and sideband Write is the band byte prefix.
//
// Callers that need exactly one pkt-line per Write call should use the
// top-level [Write] function, which errors on payloads exceeding
// [MaxPayloadSize].
type Writer struct {
	w        io.Writer
	sideband bool
	maxSize  int // maxSize on-wire pkt-line size (DefaultSize or MaxSize)
}

// NewWriter returns a Writer that emits plain pkt-lines. Payloads
// larger than [MaxPayloadSize] are split across multiple pkt-lines.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w, maxSize: MaxSize}
}

// NewSidebandWriter returns a Writer in sideband mode. Write emits
// [BandData] pkt-lines bounded by maxSize total bytes (4-byte length header
// + 1-byte band byte + payload). Use [DefaultSize] for legacy
// side-band or [MaxSize] for sideband-64k.
func NewSidebandWriter(w io.Writer, maxSize int) *Writer {
	return &Writer{w: w, sideband: true, maxSize: maxSize}
}

// Write implements io.Writer. In sideband mode, each pkt-line is
// prefixed with the [BandData] byte.
func (wr *Writer) Write(p []byte) (int, error) {
	if wr.w == nil {
		return 0, ErrNilWriter
	}
	if wr.sideband {
		return WriteSideband(wr.w, BandData, p, wr.maxSize)
	}
	return writeChunked(wr.w, p, wr.maxSize)
}

// WriteProgress writes progress messages. In sideband mode they are
// emitted as [BandProgress] pkt-lines; in plain mode WriteProgress is a
// no-op (progress has no non-sideband encoding) and returns (len(p), nil).
func (wr *Writer) WriteProgress(p []byte) (int, error) {
	if wr.w == nil {
		return 0, ErrNilWriter
	}
	if !wr.sideband {
		return len(p), nil
	}
	return WriteSideband(wr.w, BandProgress, p, wr.maxSize)
}

// Flush writes a flush-pkt (0000).
func (wr *Writer) Flush() error {
	return WriteFlush(wr.w)
}

// Delim writes a delim-pkt (0001).
func (wr *Writer) Delim() error {
	return WriteDelim(wr.w)
}

// writeChunked writes p as one or more plain pkt-lines, each bounded by
// maxSize total on-wire bytes (length header + payload).
func writeChunked(w io.Writer, p []byte, maxSize int) (int, error) {
	if maxSize <= LenSize {
		return 0, errors.New("pktline: maxSize packet size too small")
	}
	chunk := maxSize - LenSize
	if len(p) == 0 {
		// Preserve empty-payload semantics of the top-level Write.
		return Write(w, p)
	}
	written := 0
	for len(p) > 0 {
		n := min(len(p), chunk)
		if _, err := Write(w, p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

// WriteSideband writes one or more sideband pkt-lines carrying payload
// on the given band. Large payloads are split across multiple pkt-lines,
// each bounded by maxSize total on-wire bytes (4-byte length header +
// 1-byte band byte + payload chunk). Use [DefaultSize] for legacy
// side-band or [MaxSize] for sideband-64k.
//
// The returned byte count is the number of payload bytes written
// (excluding band bytes and length headers).
func WriteSideband(w io.Writer, band byte, payload []byte, maxSize int) (int, error) {
	if w == nil {
		return 0, ErrNilWriter
	}
	if maxSize <= LenSize+1 {
		return 0, errors.New("pktline: maxSize packet size too small for sideband")
	}
	chunk := maxSize - LenSize - 1
	if len(payload) == 0 {
		_, err := Write(w, []byte{band})
		return 0, err
	}
	buf := make([]byte, 0, maxSize-LenSize)
	written := 0
	for len(payload) > 0 {
		n := min(len(payload), chunk)
		buf = append(buf[:0], band)
		buf = append(buf, payload[:n]...)
		if _, err := Write(w, buf); err != nil {
			return written, err
		}
		written += n
		payload = payload[n:]
	}
	return written, nil
}
