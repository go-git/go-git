package objfile

import (
	"bufio"
	"errors"
	"io"
	"strconv"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/utils/sync"
)

// Errors returned by the objfile package.
var (
	ErrClosed        = errors.New("objfile: already closed")
	ErrHeader        = errors.New("objfile: invalid header")
	ErrHeaderTooLong = errors.New("objfile: header exceeds maximum length")
	ErrHeaderNotRead = errors.New("objfile: Header must be called before Read")
	ErrNegativeSize  = errors.New("objfile: negative object size")
	// ErrTrailingData is returned by Reader.Read when data follows the zlib
	// stream of a loose object. Detecting it relies on the zlib reader
	// stopping at the end of its stream, as the standard library does.
	ErrTrailingData = errors.New("objfile: garbage at end of loose object")
)

// maxHeaderLen mirrors canonical Git's MAX_HEADER_LEN [1]. The type,
// delimiter, size, and trailing NUL of a loose-object header must fit
// within this many inflated bytes.
//
// [1]: https://github.com/git/git/blob/v2.54.0/object-file.c#L34
const maxHeaderLen = 32

// Reader reads and decodes compressed objfile data from a provided io.Reader.
// Reader implements io.ReadCloser. Close should be called when finished with
// the Reader. Close will not close the underlying io.Reader. Reader may read
// past the end of the object from the underlying io.Reader.
type Reader struct {
	multi        io.Reader
	src          *bufio.Reader // buffers the underlying io.Reader for zlib and checkEnd
	zlib         *sync.ZLibReader
	hasher       plumbing.Hasher
	objectFormat format.ObjectFormat
	pending      int64 // number of unread bytes of the declared size
	readErr      error // returned by every Read after the stream ends or fails
	closed       bool
}

// NewReader returns a new Reader reading from r and hashing objects with the
// given object format.
func NewReader(r io.Reader, objectFormat format.ObjectFormat) (*Reader, error) {
	// A zlib reader given an io.ByteReader reads no further than the end of
	// the zlib stream, which lets checkEnd find data that follows it.
	src := sync.GetBufioReader(r)
	zlib, err := sync.GetZlibReader(src)
	if err != nil {
		sync.PutZlibReader(zlib)
		sync.PutBufioReader(src)
		return nil, packfile.ErrZLib.AddDetails("%s", err.Error())
	}

	return &Reader{
		src:          src,
		zlib:         zlib,
		objectFormat: objectFormat,
	}, nil
}

// Header reads the type and the size of object, and prepares the reader for read.
// Like canonical Git's parse_loose_header [1], it accepts only a size in
// canonical decimal form, and returns ErrHeader for a sign, a leading zero or
// any other byte. It returns ErrClosed if the Reader has been closed.
//
// [1]: https://github.com/git/git/blob/v2.54.0/object-file.c#L365-L391
func (r *Reader) Header() (t plumbing.ObjectType, size int64, err error) {
	if r.closed {
		return t, size, ErrClosed
	}
	budget := maxHeaderLen

	var raw []byte
	raw, budget, err = r.readUntil(' ', budget)
	if err != nil {
		return t, size, err
	}

	t, err = plumbing.ParseObjectType(string(raw))
	if err != nil {
		return t, size, err
	}

	raw, _, err = r.readUntil(0, budget)
	if err != nil {
		return t, size, err
	}

	if len(raw) == 0 || (raw[0] == '0' && len(raw) > 1) {
		return t, 0, ErrHeader
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return t, 0, ErrHeader
		}
	}
	size, err = strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return t, 0, ErrHeader
	}

	defer r.prepareForRead(t, size)
	return t, size, err
}

// readUntil reads one inflated byte at a time from r.zlib until it encounters
// delim, the budget is exhausted, or an error. budget caps the total number
// of bytes consumed from r.zlib, including delim; it mirrors canonical Git's
// MAX_HEADER_LEN bound applied across the full loose-object header.
func (r *Reader) readUntil(delim byte, budget int) ([]byte, int, error) {
	var buf [1]byte
	value := make([]byte, 0, 16)
	for {
		if budget <= 0 {
			return nil, 0, ErrHeaderTooLong
		}
		if n, err := r.zlib.Read(buf[:]); err != nil && (err != io.EOF || n == 0) {
			if err == io.EOF {
				return nil, 0, ErrHeader
			}
			return nil, 0, err
		}
		budget--

		if buf[0] == delim {
			return value, budget, nil
		}

		value = append(value, buf[0])
	}
}

func (r *Reader) prepareForRead(t plumbing.ObjectType, size int64) {
	r.hasher = plumbing.NewHasher(r.objectFormat, t, size)
	r.multi = io.TeeReader(r.zlib, r.hasher)
	r.pending = size
}

// Read reads up to len(p) bytes of the object data into p. It never returns
// more bytes than the size declared in the header, and returns
// io.ErrUnexpectedEOF if the data stream holds fewer.
//
// Once the declared size has been read, Read returns io.EOF if the object
// ends there, ErrOverflow if the data stream holds more bytes, and
// ErrTrailingData if data follows the zlib stream. A caller that stops after
// the declared size, as io.ReadFull does, skips these checks. After an error,
// every later call returns the same error.
//
// Read returns ErrHeaderNotRead if Header has not been called successfully,
// and ErrClosed if the Reader has been closed.
func (r *Reader) Read(p []byte) (n int, err error) {
	if r.closed {
		return 0, ErrClosed
	}
	if r.multi == nil {
		return 0, ErrHeaderNotRead
	}
	if r.readErr != nil {
		return 0, r.readErr
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.pending == 0 {
		r.readErr = r.checkEnd()
		return 0, r.readErr
	}

	if int64(len(p)) > r.pending {
		p = p[:r.pending]
	}
	n, err = r.multi.Read(p)
	r.pending -= int64(n)
	if err == io.EOF {
		if r.pending > 0 {
			err = io.ErrUnexpectedEOF
		} else {
			err = r.checkEnd()
		}
	}
	if err != nil {
		r.readErr = err
	}
	return n, err
}

// checkEnd reports whether the object ends exactly at the declared size,
// as canonical Git's unpack_loose_rest does [1]. The data stream must end
// there, and no data may follow the zlib stream. It reads past the hasher,
// so any extra byte does not affect Hash.
//
// [1]: https://github.com/git/git/blob/v2.54.0/object-file.c#L318-L326
func (r *Reader) checkEnd() error {
	var buf [1]byte
	n, err := io.ReadFull(r.zlib, buf[:])
	if n > 0 {
		return ErrOverflow
	}
	if err != io.EOF {
		return err
	}
	if _, err := r.src.ReadByte(); err != io.EOF {
		if err != nil {
			return err
		}
		return ErrTrailingData
	}
	return io.EOF
}

// Hash returns the hash of the object data stream that has been read so far.
// It returns a zero plumbing.Hash carrying the Reader's configured object
// format if Header has not been called successfully — the format matters
// because [plumbing.Hash] encodes it internally and the result feeds
// serialisers that emit a format-sized byte slice.
func (r *Reader) Hash() plumbing.Hash {
	if r.multi == nil {
		var h plumbing.Hash
		h.ResetBySize(r.objectFormat.Size())
		return h
	}
	return r.hasher.Sum()
}

// Close releases any resources consumed by the Reader. Calling Close does not
// close the wrapped io.Reader originally passed to NewReader.
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true

	defer sync.PutBufioReader(r.src)
	defer sync.PutZlibReader(r.zlib)
	return r.zlib.Close()
}
