package packfile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
)

// See https://github.com/git/git/blob/49fa3dc76179e04b0833542fa52d0f287a4955ac/delta.h
// https://github.com/git/git/blob/c2c5f6b1e479f2c38e0e01345350620944e3527f/patch-delta.c,
// and https://github.com/tarruda/node-git-core/blob/master/src/js/delta.js
// for details about the delta format.

// Delta errors.
var (
	ErrInvalidDelta = errors.New("invalid delta")
	ErrDeltaCmd     = errors.New("wrong delta command")
)

// minDeltaSize is the smallest valid delta: a 1-byte srcSz LEB128
// header followed by a 1-byte targetSz LEB128 header (the shortest case
// being targetSz=0 with no operations).
const minDeltaSize = 2

type offset struct {
	mask  byte
	shift uint
}

var offsets = []offset{
	{mask: 0x01, shift: 0},
	{mask: 0x02, shift: 8},
	{mask: 0x04, shift: 16},
	{mask: 0x08, shift: 24},
}

var sizes = []offset{
	{mask: 0x10, shift: 0},
	{mask: 0x20, shift: 8},
	{mask: 0x40, shift: 16},
}

// ApplyDelta writes to target the result of applying the modification deltas in delta to base.
func ApplyDelta(target, base plumbing.EncodedObject, delta *bytes.Buffer) (err error) {
	r, err := base.Reader()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(r, &err)

	w, err := target.Writer()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(w, &err)

	buf := sync.GetBytesBuffer()
	defer sync.PutBytesBuffer(buf)
	_, err = buf.ReadFrom(r)
	if err != nil {
		return err
	}
	src := buf.Bytes()

	dst := sync.GetBytesBuffer()
	defer sync.PutBytesBuffer(dst)
	err = patchDelta(dst, src, delta.Bytes())
	if err != nil {
		return err
	}

	target.SetSize(int64(dst.Len()))

	_, err = ioutil.CopyBufferPool(w, dst)
	return err
}

// PatchDelta returns the result of applying the modification deltas in delta to src.
// An error will be returned if delta is corrupted (ErrInvalidDelta) or an action command
// is not copy from source or copy from delta (ErrDeltaCmd).
func PatchDelta(src, delta []byte) ([]byte, error) {
	if len(src) == 0 || len(delta) < minDeltaSize {
		return nil, ErrInvalidDelta
	}

	b := &bytes.Buffer{}
	if err := patchDelta(b, src, delta); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ReaderFromDelta returns a reader that applies a delta to a base object.
//
// The delta payload is read in full and its operations checked against
// the target size the header advertises before any of the target is
// produced, so that a delta which cannot deliver what it advertises is
// rejected here rather than through the returned reader; see
// validateDeltaOps. Only the payload is held. The base is still read
// through as the operations call for it, which is where the bulk of a
// delta application's reads are.
func ReaderFromDelta(base plumbing.EncodedObject, deltaRC io.Reader) (io.ReadCloser, error) {
	// Pooled rather than io.ReadAll, which doubles its buffer as it
	// fills and so allocates several times the bytes it ends up
	// holding. Ownership passes to the goroutine below once it starts,
	// because the operations read straight out of this buffer; until
	// then every return path has to hand it back.
	payload := sync.GetBytesBuffer()
	handedOff := false
	defer func() {
		if !handedOff {
			sync.PutBytesBuffer(payload)
		}
	}()

	if _, err := payload.ReadFrom(deltaRC); err != nil {
		return nil, err
	}

	delta := payload.Bytes()
	if len(delta) < minDeltaSize {
		return nil, ErrInvalidDelta
	}

	srcSz, delta, err := decodeDeltaSize(delta)
	if err != nil {
		return nil, err
	}
	if srcSz != uint(base.Size()) {
		return nil, ErrInvalidDelta
	}

	targetSz, delta, err := decodeDeltaSize(delta)
	if err != nil {
		return nil, err
	}

	if err := validateDeltaOps(delta, srcSz, targetSz); err != nil {
		return nil, err
	}

	remainingTargetSz := targetSz
	deltaBuf := bytes.NewReader(delta)

	dstRd, dstWr := io.Pipe()

	handedOff = true
	go func() {
		defer sync.PutBytesBuffer(payload)

		baseRd, err := base.Reader()
		if err != nil {
			_ = dstWr.CloseWithError(ErrInvalidDelta)
			return
		}
		defer func() { _ = baseRd.Close() }()

		baseBuf := bufio.NewReader(baseRd)
		basePos := uint(0)

		for remainingTargetSz > 0 {
			cmd, err := deltaBuf.ReadByte()
			if err == io.EOF {
				_ = dstWr.CloseWithError(ErrInvalidDelta)
				return
			}
			if err != nil {
				_ = dstWr.CloseWithError(err)
				return
			}

			switch {
			case isCopyFromSrc(cmd):
				offset, err := decodeOffsetByteReader(cmd, deltaBuf)
				if err != nil {
					_ = dstWr.CloseWithError(err)
					return
				}
				sz, err := decodeSizeByteReader(cmd, deltaBuf)
				if err != nil {
					_ = dstWr.CloseWithError(err)
					return
				}

				if invalidSize(sz, remainingTargetSz) ||
					invalidOffsetSize(offset, sz, srcSz) {
					_ = dstWr.CloseWithError(ErrInvalidDelta)
					return
				}

				discard := offset - basePos
				if basePos > offset {
					_ = baseRd.Close()
					baseRd, err = base.Reader()
					if err != nil {
						_ = dstWr.CloseWithError(ErrInvalidDelta)
						return
					}
					baseBuf.Reset(baseRd)
					discard = offset
				}
				for discard > math.MaxInt32 {
					n, err := baseBuf.Discard(math.MaxInt32)
					if err != nil {
						_ = dstWr.CloseWithError(err)
						return
					}
					basePos += uint(n)
					discard -= uint(n)
				}
				for discard > 0 {
					n, err := baseBuf.Discard(int(discard))
					if err != nil {
						_ = dstWr.CloseWithError(err)
						return
					}
					basePos += uint(n)
					discard -= uint(n)
				}
				n, err := ioutil.CopyBufferPool(dstWr, io.LimitReader(baseBuf, int64(sz)))
				if err != nil {
					_ = dstWr.CloseWithError(err)
					return
				}
				// A base that yields fewer bytes than its size reported
				// would otherwise truncate the target silently.
				if uint(n) != sz {
					_ = dstWr.CloseWithError(ErrInvalidDelta)
					return
				}
				remainingTargetSz -= sz
				basePos += sz

			case isCopyFromDelta(cmd):
				sz := uint(cmd) // cmd is the size itself
				if invalidSize(sz, remainingTargetSz) {
					_ = dstWr.CloseWithError(ErrInvalidDelta)
					return
				}
				n, err := ioutil.CopyBufferPool(dstWr, io.LimitReader(deltaBuf, int64(sz)))
				if err != nil {
					_ = dstWr.CloseWithError(err)
					return
				}
				// Mirror patchDelta's length check on the payload: a
				// short read here would truncate the target silently.
				if uint(n) != sz {
					_ = dstWr.CloseWithError(ErrInvalidDelta)
					return
				}

				remainingTargetSz -= sz

			default:
				_ = dstWr.CloseWithError(ErrDeltaCmd)
				return
			}
		}

		// Mirror upstream's `data != top` post-loop check: every byte
		// of the delta payload must be consumed.
		if _, err := deltaBuf.ReadByte(); err == nil {
			_ = dstWr.CloseWithError(ErrInvalidDelta)
			return
		} else if err != io.EOF {
			_ = dstWr.CloseWithError(err)
			return
		}

		_ = dstWr.Close()
	}()

	return dstRd, nil
}

// decodeDeltaSize decodes one LEB128-encoded size from the front of
// delta and returns it along with the bytes that follow it.
//
// [packutil.DecodeLEB128] stops at the end of its input without
// complaint, so a size whose last byte still carries the continuation
// bit decodes to whatever partial value it had accumulated instead of
// reporting the truncation. Both header fields are read through here so
// that a header running off the end of the payload is rejected before
// anything is sized from it.
func decodeDeltaSize(delta []byte) (uint, []byte, error) {
	sz, rest, err := packutil.DecodeLEB128(delta)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %w", ErrInvalidDelta, err)
	}

	// A well-formed size ends on a byte with the continuation bit clear.
	// Nothing consumed means there was no size to read; the comparison
	// is not an equality so that the index below stays in range whatever
	// DecodeLEB128 hands back.
	consumed := len(delta) - len(rest)
	if consumed <= 0 || delta[consumed-1]&maskContinue != 0 {
		return 0, nil, ErrInvalidDelta
	}

	return sz, rest, nil
}

// validateDeltaOps reports whether the operation stream in delta builds
// a target of exactly targetSz bytes out of a source of srcSz bytes. It
// reads no source bytes, produces no output, and allocates nothing.
//
// A delta header advertises the size of its target, but that size is
// only reachable if the operations that follow add up to it. Applying a
// delta on the strength of the advertised size alone means a shortfall
// surfaces only once the payload runs out, by which point the target
// has been built and is then discarded. A single operation byte can
// copy up to maxCopySize bytes out of the source, so a delta of a few
// kilobytes is enough to drive an allocation of several gigabytes that
// way. Validating first bounds the work a delta can ask for by the work
// its operations account for.
//
// The checks below mirror those the operation loops make as they run,
// in the same order and with the same errors, so that a delta accepted
// here is one they would have accepted anyway and a rejected one
// reports what they would have reported. This pass decides only whether
// the loops run at all; it does not relieve them of their own bounds
// checks.
//
// A targetSz past [math.MaxInt] is rejected outright, so callers may
// use a validated targetSz as a length.
func validateDeltaOps(delta []byte, srcSz, targetSz uint) error {
	if targetSz > math.MaxInt {
		return ErrInvalidDelta
	}

	remainingTargetSz := targetSz

	for remainingTargetSz > 0 {
		if len(delta) == 0 {
			return ErrInvalidDelta
		}

		cmd := delta[0]
		delta = delta[1:]

		switch {
		case isCopyFromSrc(cmd):
			var offset, sz uint
			var err error
			offset, delta, err = decodeOffset(cmd, delta)
			if err != nil {
				return err
			}

			sz, delta, err = decodeSize(cmd, delta)
			if err != nil {
				return err
			}

			if invalidSize(sz, remainingTargetSz) ||
				invalidOffsetSize(offset, sz, srcSz) {
				return ErrInvalidDelta
			}
			remainingTargetSz -= sz

		case isCopyFromDelta(cmd):
			sz := uint(cmd) // cmd is the size itself
			if invalidSize(sz, remainingTargetSz) {
				return ErrInvalidDelta
			}

			if uint(len(delta)) < sz {
				return ErrInvalidDelta
			}

			remainingTargetSz -= sz
			delta = delta[sz:]

		default:
			return ErrDeltaCmd
		}
	}

	// Mirror upstream's `data != top` post-loop check: every byte of
	// the delta payload must be consumed.
	if len(delta) != 0 {
		return ErrInvalidDelta
	}

	return nil
}

func patchDelta(dst *bytes.Buffer, src, delta []byte) error {
	srcSz, delta, err := decodeDeltaSize(delta)
	if err != nil {
		return err
	}
	if srcSz != uint(len(src)) {
		return ErrInvalidDelta
	}

	targetSz, delta, err := decodeDeltaSize(delta)
	if err != nil {
		return err
	}

	if err := validateDeltaOps(delta, srcSz, targetSz); err != nil {
		return err
	}

	remainingTargetSz := targetSz

	// targetSz has been validated against the operation stream, so it is
	// safe to make room for all of it at once instead of growing into it.
	dst.Grow(int(targetSz))

	for remainingTargetSz > 0 {
		if len(delta) == 0 {
			return ErrInvalidDelta
		}

		cmd := delta[0]
		delta = delta[1:]

		switch {
		case isCopyFromSrc(cmd):
			var offset, sz uint
			var err error
			offset, delta, err = decodeOffset(cmd, delta)
			if err != nil {
				return err
			}

			sz, delta, err = decodeSize(cmd, delta)
			if err != nil {
				return err
			}

			if invalidSize(sz, remainingTargetSz) ||
				invalidOffsetSize(offset, sz, srcSz) {
				return ErrInvalidDelta
			}
			dst.Write(src[offset : offset+sz])
			remainingTargetSz -= sz

		case isCopyFromDelta(cmd):
			sz := uint(cmd) // cmd is the size itself
			if invalidSize(sz, remainingTargetSz) {
				return ErrInvalidDelta
			}

			if uint(len(delta)) < sz {
				return ErrInvalidDelta
			}

			dst.Write(delta[0:sz])
			remainingTargetSz -= sz
			delta = delta[sz:]

		default:
			return ErrDeltaCmd
		}
	}

	// Mirror upstream's `data != top` post-loop check: every byte of
	// the delta payload must be consumed.
	if len(delta) != 0 {
		return ErrInvalidDelta
	}

	return nil
}

// patchDeltaWriter applies delta to the first baseSz bytes of base,
// writing the result to dst and reporting the size and hash of the
// object it produced.
//
// baseSz is passed separately because [io.ReaderAt] does not report a
// size, and the source size the delta header advertises has to be
// checked against the source actually supplied before the operations
// and the destination are sized from it.
func patchDeltaWriter(dst io.Writer, base io.ReaderAt, baseSz int64, delta []byte,
	typ plumbing.ObjectType, writeHeader objectHeaderWriter, of format.ObjectFormat,
) (uint, plumbing.Hash, error) {
	if len(delta) < minDeltaSize {
		return 0, plumbing.ZeroHash, ErrInvalidDelta
	}

	srcSz, delta, err := decodeDeltaSize(delta)
	if err != nil {
		return 0, plumbing.ZeroHash, err
	}

	if baseSz < 0 || srcSz != uint(baseSz) {
		return 0, plumbing.ZeroHash, ErrInvalidDelta
	}

	targetSz, delta, err := decodeDeltaSize(delta)
	if err != nil {
		return 0, plumbing.ZeroHash, err
	}

	if err := validateDeltaOps(delta, srcSz, targetSz); err != nil {
		return 0, plumbing.ZeroHash, err
	}

	// Avoid several interactions expanding the buffer, which can be quite
	// inefficient on large deltas. targetSz has been validated against the
	// operation stream, so all of it can be made available at once.
	if b, ok := dst.(*bytes.Buffer); ok {
		b.Grow(int(targetSz))
	}

	// If header still needs to be written, caller will provide
	// a LazyObjectWriterHeader. This seems to be the case when
	// dealing with thin-packs.
	if writeHeader != nil {
		err = writeHeader(typ, int64(targetSz))
		if err != nil {
			return 0, plumbing.ZeroHash, fmt.Errorf("could not lazy write header: %w", err)
		}
	}

	remainingTargetSz := targetSz

	hasher := plumbing.NewHasher(of, typ, int64(targetSz))
	mw := io.MultiWriter(dst, hasher)

	bufp := sync.GetByteSlice()
	defer sync.PutByteSlice(bufp)

	deltaBuf := bytes.NewReader(delta)
	sr := io.NewSectionReader(base, int64(0), int64(srcSz))
	// Keep both the io.LimitedReader types, so we can reset N.
	baselr := io.LimitReader(sr, 0).(*io.LimitedReader)
	deltalr := io.LimitReader(deltaBuf, 0).(*io.LimitedReader)

	for remainingTargetSz > 0 {
		buf := *bufp
		cmd, err := deltaBuf.ReadByte()
		if err == io.EOF {
			return 0, plumbing.ZeroHash, ErrInvalidDelta
		}
		if err != nil {
			return 0, plumbing.ZeroHash, err
		}

		switch {
		case isCopyFromSrc(cmd):
			offset, err := decodeOffsetByteReader(cmd, deltaBuf)
			if err != nil {
				return 0, plumbing.ZeroHash, err
			}
			sz, err := decodeSizeByteReader(cmd, deltaBuf)
			if err != nil {
				return 0, plumbing.ZeroHash, err
			}

			if invalidSize(sz, remainingTargetSz) ||
				invalidOffsetSize(offset, sz, srcSz) {
				return 0, plumbing.ZeroHash, ErrInvalidDelta
			}

			if _, err := sr.Seek(int64(offset), io.SeekStart); err != nil {
				return 0, plumbing.ZeroHash, err
			}
			baselr.N = int64(sz)
			if _, err := io.CopyBuffer(mw, baselr, buf); err != nil {
				return 0, plumbing.ZeroHash, err
			}
			remainingTargetSz -= sz
		case isCopyFromDelta(cmd):
			sz := uint(cmd) // cmd is the size itself
			if invalidSize(sz, remainingTargetSz) {
				return 0, plumbing.ZeroHash, ErrInvalidDelta
			}
			deltalr.N = int64(sz)
			if _, err := io.CopyBuffer(mw, deltalr, buf); err != nil {
				return 0, plumbing.ZeroHash, err
			}

			remainingTargetSz -= sz
		default:
			return 0, plumbing.ZeroHash, ErrDeltaCmd
		}
	}

	// Mirror upstream's `data != top` post-loop check: every byte of
	// the delta payload must be consumed.
	if _, err := deltaBuf.ReadByte(); err == nil {
		return 0, plumbing.ZeroHash, ErrInvalidDelta
	} else if err != io.EOF {
		return 0, plumbing.ZeroHash, err
	}

	return targetSz, hasher.Sum(), nil
}

func isCopyFromSrc(cmd byte) bool {
	return (cmd & maskContinue) != 0
}

func isCopyFromDelta(cmd byte) bool {
	return (cmd&maskContinue) == 0 && cmd != 0
}

func decodeOffsetByteReader(cmd byte, delta io.ByteReader) (uint, error) {
	var offset uint
	for _, o := range offsets {
		if (cmd & o.mask) != 0 {
			next, err := delta.ReadByte()
			if err != nil {
				return 0, err
			}
			offset |= uint(next) << o.shift
		}
	}

	return offset, nil
}

func decodeOffset(cmd byte, delta []byte) (uint, []byte, error) {
	var offset uint
	for _, o := range offsets {
		if (cmd & o.mask) != 0 {
			if len(delta) == 0 {
				return 0, nil, ErrInvalidDelta
			}
			offset |= uint(delta[0]) << o.shift
			delta = delta[1:]
		}
	}

	return offset, delta, nil
}

func decodeSizeByteReader(cmd byte, delta io.ByteReader) (uint, error) {
	var sz uint
	for _, s := range sizes {
		if (cmd & s.mask) != 0 {
			next, err := delta.ReadByte()
			if err != nil {
				return 0, err
			}
			sz |= uint(next) << s.shift
		}
	}

	if sz == 0 {
		sz = maxCopySize
	}

	return sz, nil
}

func decodeSize(cmd byte, delta []byte) (uint, []byte, error) {
	var sz uint
	for _, s := range sizes {
		if (cmd & s.mask) != 0 {
			if len(delta) == 0 {
				return 0, nil, ErrInvalidDelta
			}
			sz |= uint(delta[0]) << s.shift
			delta = delta[1:]
		}
	}
	if sz == 0 {
		sz = maxCopySize
	}

	return sz, delta, nil
}

// invalidSize reports whether sz exceeds the remaining target size.
func invalidSize(sz, remaining uint) bool {
	return sz > remaining
}

func invalidOffsetSize(offset, sz, srcSz uint) bool {
	return sumOverflows(offset, sz) ||
		offset+sz > srcSz
}

func sumOverflows(a, b uint) bool {
	return a+b < a
}
