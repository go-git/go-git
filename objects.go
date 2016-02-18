package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"gopkg.in/src-d/go-git.v3/core"
)

// Blob is used to store file data - it is generally a file.
type Blob struct {
	Hash core.Hash
	Size int64

	obj core.Object
}

var ErrUnsupportedObject = errors.New("unsupported object type")

// Decode transform an core.Object into a Blob struct
func (b *Blob) Decode(o core.Object) error {
	if o.Type() != core.BlobObject {
		return ErrUnsupportedObject
	}

	b.Hash = o.Hash()
	b.Size = o.Size()
	b.obj = o

	return nil
}

// Reader returns a reader allow the access to the content of the blob
func (b *Blob) Reader() io.Reader {
	return b.obj.Reader()
}

// Signature represents an action signed by a person
type Signature struct {
	Name  string
	Email string
	When  time.Time
}

// Decode decodes a byte slice into a signature
func (s *Signature) Decode(b []byte) {
	open := bytes.IndexByte(b, '<')
	close := bytes.IndexByte(b, '>')
	if open == -1 || close == -1 {
		return
	}

	s.Name = string(bytes.Trim(b[:open], " "))
	s.Email = string(b[open+1 : close])

	hasTime := close+2 < len(b)
	if hasTime {
		s.decodeTimeAndTimeZone(b[close+2:])
	}
}

var timeZoneLength = 5

func (s *Signature) decodeTimeAndTimeZone(b []byte) {
	space := bytes.IndexByte(b, ' ')
	if space == -1 {
		space = len(b)
	}

	ts, err := strconv.ParseInt(string(b[:space]), 10, 64)
	if err != nil {
		return
	}

	s.When = time.Unix(ts, 0).In(time.UTC)
	var tzStart = space + 1
	if tzStart >= len(b) || tzStart+timeZoneLength > len(b) {
		return
	}

	tl, err := time.Parse("-0700", string(b[tzStart:tzStart+timeZoneLength]))
	if err != nil {
		return
	}

	s.When = s.When.In(tl.Location())
}

func (s *Signature) String() string {
	return fmt.Sprintf("%s <%s>", s.Name, s.Email)
}
