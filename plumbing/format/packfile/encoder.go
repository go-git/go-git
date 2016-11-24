package packfile

import (
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/utils/binary"
)

// Encoder gets the data from the storage and write it into the writer in PACK
// format
type Encoder struct {
	storage storer.ObjectStorer
	w       io.Writer
	zw      *zlib.Writer
	hasher  plumbing.Hasher
}

// NewEncoder creates a new packfile encoder using a specific Writer and
// ObjectStorer
func NewEncoder(w io.Writer, s storer.ObjectStorer) *Encoder {
	h := plumbing.Hasher{
		Hash: sha1.New(),
	}
	mw := io.MultiWriter(w, h)
	zw := zlib.NewWriter(mw)
	return &Encoder{
		storage: s,
		w:       mw,
		zw:      zw,
		hasher:  h,
	}
}

// Encode creates a packfile containing all the objects referenced in hashes
// and writes it to the writer in the Encoder.
func (e *Encoder) Encode(hashes []plumbing.Hash) (plumbing.Hash, error) {
	if err := e.head(len(hashes)); err != nil {
		return plumbing.ZeroHash, err
	}

	for _, h := range hashes {
		o, err := e.storage.Object(plumbing.AnyObject, h)
		if err != nil {
			return plumbing.ZeroHash, err
		}

		if err := e.entry(o); err != nil {
			return plumbing.ZeroHash, err
		}
	}

	return e.footer()
}

func (e *Encoder) head(numEntries int) error {
	return binary.Write(
		e.w,
		signature,
		int32(VersionSupported),
		int32(numEntries),
	)
}

func (e *Encoder) entry(o plumbing.Object) error {
	t := o.Type()
	if t == plumbing.OFSDeltaObject || t == plumbing.REFDeltaObject {
		// TODO implements delta objects
		return fmt.Errorf("delta object not supported: %v", t)
	}

	if err := e.entryHead(t, o.Size()); err != nil {
		return err
	}

	e.zw.Reset(e.w)
	or, err := o.Reader()
	if err != nil {
		return err
	}
	_, err = io.Copy(e.zw, or)
	if err != nil {
		return err
	}

	return e.zw.Close()
}

func (e *Encoder) entryHead(typeNum plumbing.ObjectType, size int64) error {
	t := int64(typeNum)
	header := []byte{}
	c := (t << firstLengthBits) | (size & maskFirstLength)
	size >>= firstLengthBits
	for {
		if size == 0 {
			break
		}
		header = append(header, byte(c|maskContinue))
		c = size & int64(maskLength)
		size >>= lengthBits
	}

	header = append(header, byte(c))
	_, err := e.w.Write(header)

	return err
}

func (e *Encoder) footer() (plumbing.Hash, error) {
	h := e.hasher.Sum()
	return h, binary.Write(e.w, h)
}
