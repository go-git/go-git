package packfile

import (
	"bufio"
	"io"
)

// scannerReader has the following characteristics:
//   - Provides an io.SeekReader impl for bufio.Reader, when the underlying
//     reader supports it.
//   - Keeps track of the current read position, for when the underlying reader
//     isn't an io.SeekReader, but we still want to know the current offset.
//   - Writes to the hash writer what it reads, with the aid of a smaller buffer.
//     The buffer helps avoid a performance penalty for performing small writes
//     to the crc32 hash writer.
//
// Note that this is passed on to zlib, and it mmust support io.BytesReader, else
// it won't be able to just read the content of the current object, but rather it
// will read the entire packfile.
//
// scannerReader is not thread-safe.
type scannerReader struct {
	reader io.Reader
	crc    io.Writer
	rbuf   *bufio.Reader
	wbuf   *bufio.Writer
	offset int64
	seeker io.Seeker
}

func newScannerReader(r io.Reader, h io.Writer) *scannerReader {
	sr := &scannerReader{
		rbuf: bufio.NewReader(nil),
		wbuf: bufio.NewWriterSize(nil, 64),
		crc:  h,
	}
	sr.Reset(r)

	return sr
}

func (r *scannerReader) Reset(reader io.Reader) {
	r.reader = reader
	r.rbuf.Reset(r.reader)
	r.wbuf.Reset(r.crc)

	r.offset = 0

	seeker, ok := r.reader.(io.ReadSeeker)
	r.seeker = seeker

	if ok {
		r.offset, _ = seeker.Seek(0, io.SeekCurrent)
	}
}

func (r *scannerReader) Read(p []byte) (n int, err error) {
	n, err = r.rbuf.Read(p)

	r.offset += int64(n)
	if _, err := r.wbuf.Write(p[:n]); err != nil {
		return n, err
	}
	return
}

func (r *scannerReader) ReadByte() (b byte, err error) {
	b, err = r.rbuf.ReadByte()
	if err == nil {
		r.offset++
		return b, r.wbuf.WriteByte(b)
	}
	return
}

func (r *scannerReader) Flush() error {
	return r.wbuf.Flush()
}

// Seek seeks to a location. If the underlying reader is not an io.ReadSeeker,
// then only whence=io.SeekCurrent is supported, any other operation fails.
func (r *scannerReader) Seek(offset int64, whence int) (int64, error) {
	var err error

	if r.seeker == nil {
		if whence != io.SeekCurrent || offset != 0 {
			return -1, ErrSeekNotSupported
		}
	}

	if whence == io.SeekCurrent && offset == 0 {
		return r.offset, nil
	}

	r.offset, err = r.seeker.Seek(offset, whence)
	r.rbuf.Reset(r.reader)

	return r.offset, err
}
