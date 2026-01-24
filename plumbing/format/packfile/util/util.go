package util

import (
	"errors"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
)

const (
	firstLengthBits = uint8(4)   // the first byte into object header has 4 bits to store the length
	maskPayload     = 0x7f       // 0111 1111
	maskContinue    = 0x80       // 1000 0000
	maskType        = uint8(112) // 0111 0000
)

// VariableLengthSize reads a variable length size from first, and uses reader
// to continue on reading until the full size is determined.
func VariableLengthSize(first byte, reader io.ByteReader) (uint64, error) {
	// Extract the first part of the size (last 3 bits of the first byte).
	size := uint64(first & 0x0F)

	// |  001xxxx | xxxxxxxx | xxxxxxxx | ...
	//
	//	 ^^^       ^^^^^^^^   ^^^^^^^^
	//	Type      Size Part 1   Size Part 2
	//
	// Check if more bytes are needed to fully determine the size.
	if first&maskContinue != 0 {
		shift := uint(4)

		if reader == nil {
			return 0, errors.New("reader is nil")
		}

		for {
			b, err := reader.ReadByte()
			if err != nil {
				return 0, err
			}

			// Add the next 7 bits to the size.
			size |= uint64(b&0x7F) << shift

			// Check if the continuation bit is set.
			if b&maskContinue == 0 {
				break
			}

			// Prepare for the next byte.
			shift += 7
		}
	}
	return size, nil
}

// ObjectType returns the plumbing.ObjectType which is represented by b.
func ObjectType(b byte) plumbing.ObjectType {
	return plumbing.ObjectType((b & maskType) >> firstLengthBits)
}

// DecodeLEB128 decodes a number encoded as an unsigned LEB128 at the
// start of some binary data and returns the decoded number and the rest
// of the bytes.
func DecodeLEB128(input []byte) (uint, []byte) {
	if len(input) == 0 {
		return 0, input
	}

	var num, sz uint
	var b byte
	for {
		b = input[sz]
		num |= (uint(b) & maskPayload) << (sz * 7) // concats 7 bits chunks
		sz++

		if uint(b)&maskContinue == 0 || sz == uint(len(input)) {
			break
		}
	}

	return num, input[sz:]
}

// DecodeLEB128 decodes a number encoded as an unsigned LEB128 at the
// start of some binary data and returns the decoded number.
func DecodeLEB128FromReader(input io.ByteReader) (uint, error) {
	var num, sz uint
	for {
		b, err := input.ReadByte()
		if err != nil {
			return 0, err
		}

		num |= (uint(b) & maskPayload) << (sz * 7) // concats 7 bits chunks
		sz++

		if uint(b)&maskContinue == 0 {
			break
		}
	}

	return num, nil
}
