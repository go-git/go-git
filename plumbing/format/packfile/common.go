package packfile

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

var signature = []byte{'P', 'A', 'C', 'K'}

const (
	// VersionSupported is the packfile version supported by this package
	VersionSupported uint32 = 2

	firstLengthBits = uint8(4)   // the first byte into object header has 4 bits to store the length
	lengthBits      = uint8(7)   // subsequent bytes has 7 bits to store the length
	maskFirstLength = 15         // 0000 1111
	maskContinue    = 0x80       // 1000 0000
	maskLength      = uint8(127) // 0111 1111
)

// UpdateObjectStorage updates the storer with the objects in the given
// packfile.
func UpdateObjectStorage(ctx context.Context, s storer.Storer, packfile io.Reader) error {
	if trace.Performance.Enabled() {
		start := time.Now()
		defer func() {
			trace.Performance.Printf("performance: %.9f s: update_obj_storage", time.Since(start).Seconds())
		}()
	}

	if pw, ok := s.(storer.PackfileWriter); ok {
		return WritePackfileToObjectStorage(ctx, pw, packfile)
	}

	of := formatcfg.DefaultObjectFormat
	if c, ok := s.(config.ConfigStorer); ok {
		cfg, err := c.Config(ctx)
		if err == nil {
			of = cfg.Extensions.ObjectFormat
		}
	}

	p := NewParser(packfile, WithStorage(s), WithObjectFormat(of))
	_, err := p.Parse(ctx)
	return err
}

// WritePackfileToObjectStorage writes all the packfile objects into the given
// object storage.
func WritePackfileToObjectStorage(
	ctx context.Context,
	sw storer.PackfileWriter,
	packfile io.Reader,
) (err error) {
	w, err := sw.PackfileWriter(ctx)
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(w, &err)

	n, err := ioutil.CopyBufferPool(w, packfile)
	if err == nil && n == 0 {
		return ErrEmptyPackfile
	}

	return err
}

// ValidateOFSDeltaBase enforces the canonical-Git invariant on an
// OFS-delta's encoded negative offset: the resolved base offset
// (deltaOffset - negativeOffset) must be strictly positive (past the
// 12-byte pack header) and strictly less than deltaOffset, since an
// OFS-delta can only reference an earlier entry in the same pack.
//
// Mirrors canonical Git's predicate in packfile.c[1]:
//
//	base_offset = delta_obj_offset - base_offset;
//	if (base_offset <= 0 || base_offset >= delta_obj_offset)
//		return 0;  /* out of bound */
//
// Returns a wrapped ErrMalformedPackfile when the bounds are violated;
// returns nil otherwise.
//
// [1]: https://github.com/git/git/blob/v2.54.0/packfile.c#L1289-L1290
func ValidateOFSDeltaBase(deltaOffset, negativeOffset int64) error {
	if negativeOffset <= 0 || negativeOffset >= deltaOffset {
		return fmt.Errorf("%w: invalid OFS delta offset", ErrMalformedPackfile)
	}
	return nil
}
