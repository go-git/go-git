package packfile

import (
	"io"

	"gopkg.in/src-d/go-git.v4/core"
)

// Format specifies if the packfile uses ref-deltas or ofs-deltas.
type Format int

// Possible values of the Format type.
const (
	UnknownFormat Format = iota
	OFSDeltaFormat
	REFDeltaFormat
)

var (
	// ErrMaxObjectsLimitReached is returned by Decode when the number
	// of objects in the packfile is higher than
	// Decoder.MaxObjectsLimit.
	ErrMaxObjectsLimitReached = NewError("max. objects limit reached")

	// ErrInvalidObject is returned by Decode when an invalid object is
	// found in the packfile.
	ErrInvalidObject = NewError("invalid git object")

	// ErrPackEntryNotFound is returned by Decode when a reference in
	// the packfile references and unknown object.
	ErrPackEntryNotFound = NewError("can't find a pack entry")

	// ErrZLib is returned by Decode when there was an error unzipping
	// the packfile contents.
	ErrZLib = NewError("zlib reading error")
)

// Decoder reads and decodes packfiles from an input stream.
type Decoder struct {
	p *Parser
	s core.ObjectStorage
}

// NewDecoder returns a new Decoder that reads from r.
func NewDecoder(r ReadRecaller) *Decoder {
	return &Decoder{
		p: NewParser(r),
	}
}

// Decode reads a packfile and stores it in the value pointed to by s.
func (d *Decoder) Decode(s core.ObjectStorage) error {
	d.s = s

	count, err := d.p.ReadHeader()
	if err != nil {
		return err
	}

	return d.readObjects(count)
}

func (d *Decoder) readObjects(count uint32) error {
	// This code has 50-80 µs of overhead per object not counting zlib inflation.
	// Together with zlib inflation, it's 400-410 µs for small objects.
	// That's 1 sec for ~2450 objects, ~4.20 MB, or ~250 ms per MB,
	// of which 12-20 % is _not_ zlib inflation (ie. is our code).
	for i := 0; i < int(count); i++ {
		start, err := d.p.Offset()
		if err != nil {
			return err
		}

		obj := d.s.NewObject()
		if err := d.p.FillObject(obj); err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		err = d.p.Remember(start, obj)
		if err != nil {
			return err
		}

		_, err = d.s.Set(obj)
		if err == io.EOF {
			break
		}
	}

	return nil
}
