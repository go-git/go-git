//go:build darwin || linux

package mmap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-billy/v6"
	"golang.org/x/sys/unix"
)

// mmapFile creates a memory-mapped region for the given file.
func mmapFile(f billy.File) ([]byte, func() error, error) {
	if f == nil {
		return nil, nil, ErrNilFile
	}

	info, err := f.Stat()
	if err != nil {
		return nil, nil, errors.Join(err, f.Close())
	}

	fd, err := getFileDescriptor(f)
	if err != nil {
		return nil, nil, err
	}

	data, err := unix.Mmap(int(fd), 0, int(info.Size()), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, nil, errors.Join(err, f.Close())
	}

	cleanup := func() error {
		return errors.Join(
			unix.Munmap(data),
			f.Close(),
		)
	}

	return data, cleanup, nil
}

// getFileDescriptor extracts the file descriptor from a billy.File.
func getFileDescriptor(f billy.File) (uintptr, error) {
	if ffd, ok := f.(billyFileDescriptor); ok {
		if v, ok := ffd.Fd(); ok {
			return v, nil
		}
	}
	if ffd, ok := f.(goFileDescriptor); ok {
		return ffd.Fd(), nil
	}
	return 0, ErrNoFileDescriptor
}

// validateFile does a quick check whether the specific file (idx, rev, pack)
// is valid at a high level. It does not verify the checksum of the entire file.
func validateFile(mmap []byte, sv uint32, sig []byte, minLen int) error {
	if minLen > len(mmap) {
		return io.EOF
	}

	if !bytes.Equal(sig, mmap[:len(sig)]) {
		return fmt.Errorf("signature mismatch")
	}

	v := binary.BigEndian.Uint32(mmap[len(sig) : len(sig)+4])
	if sv != v {
		return fmt.Errorf("unsupported version: %d", v)
	}

	return nil
}

// billyFileDescriptor represents the Fd interface for billy.File.
type billyFileDescriptor interface {
	Fd() (uintptr, bool)
}

// goFileDescriptor represents the Fd interface for os.File. This is
// needed as os.File can be used interchangeably with billy.File, however
// not all implementations of the latter support Fd - hence the distinct
// signatures.
type goFileDescriptor interface {
	Fd() uintptr
}
