package packp

import (
	"errors"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// ErrInvalidV2Advertisement is returned when the protocol v2 capability
// advertisement does not begin with a valid "version 2" line.
var ErrInvalidV2Advertisement = errors.New("invalid protocol v2 capability advertisement")

// V2Capabilities holds the capabilities advertised by a server speaking
// Git wire protocol version 2. Unlike the v0/v1 capability list, each
// capability is sent on its own pkt-line as key or key=value, and the
// value may itself contain space-separated arguments (for example
// "fetch=shallow filter wait-for-done").
type V2Capabilities struct {
	m map[string]string
}

// Decode reads a capability advertisement from r. The advertisement must
// start with a "version 2" line and is terminated by a flush packet.
func (c *V2Capabilities) Decode(r io.Reader) error {
	c.m = make(map[string]string)

	_, line, err := pktline.ReadLine(r)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(line)) != "version 2" {
		return ErrInvalidV2Advertisement
	}

	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if l == pktline.Flush {
			break
		}

		key, value, _ := strings.Cut(strings.TrimRight(string(line), "\n"), "=")
		c.m[key] = value
	}

	return nil
}

// Supports reports whether the named capability was advertised.
func (c *V2Capabilities) Supports(name string) bool {
	_, ok := c.m[name]
	return ok
}

// Get returns the raw value advertised for the named capability, or the
// empty string if the capability is absent or has no value.
func (c *V2Capabilities) Get(name string) string {
	return c.m[name]
}

// SupportsArgument reports whether the named capability advertised the
// given space-separated argument (for example whether "fetch" supports
// "wait-for-done").
func (c *V2Capabilities) SupportsArgument(name, arg string) bool {
	value, ok := c.m[name]
	if !ok {
		return false
	}
	for field := range strings.FieldsSeq(value) {
		if field == arg {
			return true
		}
	}
	return false
}
