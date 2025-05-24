package packfile

import (
	"io"
	"time"

	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
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
	maskType        = uint8(112) // 0111 0000
)

// UpdateObjectStorage updates the storer with the objects in the given
// packfile.
func UpdateObjectStorage(s storer.Storer, packfile io.Reader) error {
	start := time.Now()
	defer func() {
		trace.Performance.Printf("performance: %.9f s: update_obj_storage", time.Since(start).Seconds())
	}()

	if pw, ok := s.(storer.PackfileWriter); ok {
		return WritePackfileToObjectStorage(pw, packfile)
	}

	p := NewParser(packfile, WithStorage(s))

	_, err := p.Parse()
	return err
}

// WritePackfileToObjectStorage writes all the packfile objects into the given
// object storage.
func WritePackfileToObjectStorage(
	sw storer.PackfileWriter,
	packfile io.Reader,
) (err error) {
	w, err := sw.PackfileWriter()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(w, &err)
	var n int64

	buf := sync.GetByteSlice()
	n, err = io.CopyBuffer(w, packfile, *buf)
	sync.PutByteSlice(buf, int(n))

	if err == nil && n == 0 {
		return ErrEmptyPackfile
	}

	return err
}
