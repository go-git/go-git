package objfile

import (
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
)

// maxHeaderLen mirrors canonical Git's MAX_HEADER_LEN [1]. The type,
// delimiter, size, and trailing NUL of a loose-object header must fit
// within this many inflated bytes.
//
// [1]: https://github.com/git/git/blob/v2.54.0/object-file.c#L34
const maxHeaderLen = 32

// Reader reads and decodes compressed objfile data from a provided io.Reader.
// Reader implements io.ReadCloser. Close should be called when finished with
// the Reader. Close will not close the underlying io.Reader.
type Reader struct {
	multi        io.Reader
	zlib         *sync.ZLibReader
	hasher       plumbing.Hasher
	objectFormat format.ObjectFormat
	closed       bool
}

// NewReader returns a new Reader reading from r and hashing objects with the
// given object format.
func NewReader(r io.Reader, objectFormat format.ObjectFormat) (*Reader, error) {
	zlib, err := sync.GetZlibReader(r)
	if err != nil {
		return nil, packfile.ErrZLib.AddDetails("%s", err.Error())
	}

	return &Reader{
		zlib:         zlib,
		objectFormat: objectFormat,
	}, nil
}

// Header reads the type and the size of object, and prepares the reader for read
func (r *Reader) Header() (t plumbing.ObjectType, size int64, err error) {
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

	size, err = strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		err = ErrHeader
		return t, size, err
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
}

// Read reads len(p) bytes into p from the object data stream. It returns
// the number of bytes read (0 <= n <= len(p)) and any error encountered. Even
// if Read returns n < len(p), it may use all of p as scratch space during the
// call.
//
// If Read encounters the end of the data stream it will return err == io.EOF,
// either in the current call if n > 0 or in a subsequent call.
//
// Read returns ErrHeaderNotRead if Header has not been called successfully.
func (r *Reader) Read(p []byte) (n int, err error) {
	if r.multi == nil {
		return 0, ErrHeaderNotRead
	}
	return r.multi.Read(p)
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

	defer sync.PutZlibReader(r.zlib)
	return r.zlib.Close()
}
