package binary

import (
	"encoding/binary"
	"io"
)

// Write writes the binary representation of data into w, using BigEndian order
// https://golang.org/pkg/encoding/binary/#Write
func Write(w io.Writer, data ...interface{}) error {
	for _, v := range data {
		if err := binary.Write(w, binary.BigEndian, v); err != nil {
			return err
		}
	}

	return nil
}

// WriteUint32 writes the binary representation of a uint32 into w, in BigEndian
// order
func WriteUint32(w io.Writer, value uint32) error {
	return binary.Write(w, binary.BigEndian, value)
}

// WriteUint16 writes the binary representation of a uint16 into w, in BigEndian
// order
func WriteUint16(w io.Writer, value uint16) error {
	return binary.Write(w, binary.BigEndian, value)
}
