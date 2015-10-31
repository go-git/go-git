package git

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"gopkg.in/src-d/go-git.v2/core"
)

// Blob is used to store file data - it is generally a file.
type Blob struct {
	Hash core.Hash
	Size int64

	obj core.Object
}

// Decode transform an core.Object into a Blob struct
func (b *Blob) Decode(o core.Object) error {
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
	if len(b) == 0 {
		return
	}

	from := 0
	state := 'n' // n: name, e: email, t: timestamp, z: timezone
	for i := 0; ; i++ {
		var c byte
		var end bool
		if i < len(b) {
			c = b[i]
		} else {
			end = true
		}

		switch state {
		case 'n':
			if c == '<' || end {
				if i == 0 {
					break
				}
				s.Name = string(b[from : i-1])
				state = 'e'
				from = i + 1
			}
		case 'e':
			if c == '>' || end {
				s.Email = string(b[from:i])
				i++
				state = 't'
				from = i + 1
			}
		case 't':
			if c == ' ' || end {
				t, err := strconv.ParseInt(string(b[from:i]), 10, 64)
				if err == nil {
					s.When = time.Unix(t, 0)
				}
				end = true
			}
		}

		if end {
			break
		}
	}
}

func (s *Signature) String() string {
	return fmt.Sprintf("%s <%s>", s.Name, s.Email)
}
