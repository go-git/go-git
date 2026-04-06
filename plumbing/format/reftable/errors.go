package reftable

import "errors"

var (
	// ErrInvalidReftable is returned when a reftable file has an invalid format.
	ErrInvalidReftable = errors.New("reftable: invalid format")

	// ErrBadMagic is returned when the file does not start with the 'REFT' magic.
	ErrBadMagic = errors.New("reftable: bad magic bytes")

	// ErrUnsupportedVersion is returned for unknown reftable versions.
	ErrUnsupportedVersion = errors.New("reftable: unsupported version")

	// ErrBadCRC is returned when a footer CRC-32 check fails.
	ErrBadCRC = errors.New("reftable: CRC-32 mismatch")

	// ErrCorruptBlock is returned when a block cannot be decoded.
	ErrCorruptBlock = errors.New("reftable: corrupt block")

	// ErrReadOnly is returned when a write operation is attempted on a
	// read-only reftable storage.
	ErrReadOnly = errors.New("reftable: write operations not yet supported")
)
