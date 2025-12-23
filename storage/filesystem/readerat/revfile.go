package readerat

import (
	"fmt"

	"github.com/go-git/go-billy/v6"
)

var (
	revSignature = []byte{'R', 'I', 'D', 'X'}
	revMinLen    = int64(16)
	revSupported = uint32(1)
)

const (
	revHeader = 4 + 4 + 4
)

func (s *PackScanner) loadRevFile(rev billy.File) error {
	if rev == nil {
		return ErrNilFile
	}

	info, err := rev.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat rev file: %w", err)
	}

	s.revFile = rev
	s.revReader = rev
	s.revSize = info.Size()

	if err := validateHeader(s.revReader, revSignature, revSupported, revMinLen, s.revSize); err != nil {
		return fmt.Errorf("malformed rev file: %w", err)
	}

	return nil
}
