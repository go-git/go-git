package capability

import (
	"bytes"
)

// List represents a list of capabilities. The zero value is safe to use;
// the internal map is lazily initialized on first write.
type List struct {
	m    map[string]*entry
	sort []string
}

type entry struct {
	Name   string
	Values []string
}

// IsEmpty returns true if the List is empty
func (l *List) IsEmpty() bool {
	if l == nil {
		return true
	}
	return len(l.sort) == 0
}

// DecodeList decodes a v0/v1 space-separated capability string into the
// List. This is the format used in advertise-refs, upload-request, and
// update-request messages.
func DecodeList(raw []byte, l *List) {
	if l == nil {
		return
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}

	for len(raw) > 0 {
		var chunk []byte
		if i := bytes.IndexByte(raw, ' '); i >= 0 {
			chunk = raw[:i]
			raw = raw[i+1:]
		} else {
			chunk = raw
			raw = nil
		}

		if len(chunk) == 0 {
			continue
		}

		if before, after, ok := bytes.Cut(chunk, []byte{'='}); ok {
			l.Add(string(before), string(after))
		} else {
			l.Add(string(chunk))
		}
	}
}

// EncodeList encodes the List into a v0/v1 space-separated capability string.
// This is the format used in advertise-refs, upload-request, and
// update-request messages.
func EncodeList(l *List) []byte {
	b, _ := l.MarshalText()
	return b
}

// Get returns the values for a capability
func (l *List) Get(capability string) []string {
	if l.m == nil {
		return nil
	}
	if _, ok := l.m[capability]; !ok {
		return nil
	}

	return l.m[capability].Values
}

// Set sets a capability removing the previous values
func (l *List) Set(capability string, values ...string) {
	if _, ok := l.m[capability]; ok {
		l.m[capability].Values = l.m[capability].Values[:0]
	}
	l.Add(capability, values...)
}

func (l *List) init() {
	if l.m == nil {
		l.m = make(map[string]*entry)
	}
}

// Add adds a capability, values are optional
func (l *List) Add(c string, values ...string) {
	l.init()

	if !l.Supports(c) {
		l.m[c] = &entry{Name: c}
		l.sort = append(l.sort, c)
	}

	if len(values) == 0 {
		return
	}

	l.m[c].Values = append(l.m[c].Values, values...)
}

// Supports returns true if capability is present
func (l *List) Supports(capability string) bool {
	if l.m == nil {
		return false
	}
	_, ok := l.m[capability]
	return ok
}

// Delete deletes a capability from the List
func (l *List) Delete(capability string) {
	if !l.Supports(capability) {
		return
	}

	delete(l.m, capability)
	for i, c := range l.sort {
		if c != capability {
			continue
		}

		l.sort = append(l.sort[:i], l.sort[i+1:]...)
		return
	}
}

// All returns a slice with all defined capabilities.
func (l *List) All() []string {
	if len(l.sort) == 0 {
		return nil
	}

	cs := make([]string, len(l.sort))
	copy(cs, l.sort)

	return cs
}

// MarshalText implements encoding.TextMarshaler.
func (l *List) MarshalText() ([]byte, error) {
	return l.AppendText(nil)
}

// AppendText implements encoding.TextAppender.
func (l *List) AppendText(b []byte) ([]byte, error) {
	first := true
	for _, key := range l.sort {
		if l.m == nil {
			continue
		}
		c := l.m[key]
		if len(c.Values) == 0 {
			if !first {
				b = append(b, ' ')
			}
			first = false
			b = append(b, key...)
			continue
		}

		for _, value := range c.Values {
			if !first {
				b = append(b, ' ')
			}
			first = false
			b = append(b, key...)
			b = append(b, '=')
			b = append(b, value...)
		}
	}
	return b, nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (l *List) UnmarshalText(text []byte) error {
	DecodeList(text, l)
	return nil
}

// String generates the capabilities strings, the capabilities are sorted in
// insertion order.
func (l *List) String() string {
	b, _ := l.MarshalText()
	return string(b)
}
