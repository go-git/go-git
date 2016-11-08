package packp

import (
	"fmt"
	"sort"
	"strings"
)

// Capabilities contains all the server capabilities
// https://github.com/git/git/blob/master/Documentation/technical/protocol-capabilities.txt
type Capabilities struct {
	m map[string]*Capability
	o []string
}

// Capability represents a server capability
type Capability struct {
	Name   string
	Values []string
}

// NewCapabilities returns a new Capabilities struct
func NewCapabilities() *Capabilities {
	return &Capabilities{
		m: make(map[string]*Capability),
	}
}

func (c *Capabilities) IsEmpty() bool {
	return len(c.o) == 0
}

// Decode decodes a string
func (c *Capabilities) Decode(raw string) {
	params := strings.Split(raw, " ")
	for _, p := range params {
		s := strings.SplitN(p, "=", 2)

		var value string
		if len(s) == 2 {
			value = s[1]
		}

		c.Add(s[0], value)
	}
}

// Get returns the values for a capability
func (c *Capabilities) Get(capability string) *Capability {
	return c.m[capability]
}

// Set sets a capability removing the values
func (c *Capabilities) Set(capability string, values ...string) {
	if _, ok := c.m[capability]; ok {
		delete(c.m, capability)
	}

	c.Add(capability, values...)
}

// Add adds a capability, values are optional
func (c *Capabilities) Add(capability string, values ...string) {
	if !c.Supports(capability) {
		c.m[capability] = &Capability{Name: capability}
		c.o = append(c.o, capability)
	}

	if len(values) == 0 {
		return
	}

	c.m[capability].Values = append(c.m[capability].Values, values...)
}

// Supports returns true if capability is present
func (c *Capabilities) Supports(capability string) bool {
	_, ok := c.m[capability]
	return ok
}

// SymbolicReference returns the reference for a given symbolic reference
func (c *Capabilities) SymbolicReference(sym string) string {
	if !c.Supports("symref") {
		return ""
	}

	for _, symref := range c.Get("symref").Values {
		parts := strings.Split(symref, ":")
		if len(parts) != 2 {
			continue
		}

		if parts[0] == sym {
			return parts[1]
		}
	}

	return ""
}

// Sorts capabilities in increasing order of their name
func (c *Capabilities) Sort() {
	sort.Strings(c.o)
}

func (c *Capabilities) String() string {
	if len(c.o) == 0 {
		return ""
	}

	var o string
	for _, key := range c.o {
		cap := c.m[key]

		added := false
		for _, value := range cap.Values {
			if value == "" {
				continue
			}

			added = true
			o += fmt.Sprintf("%s=%s ", key, value)
		}

		if len(cap.Values) == 0 || !added {
			o += key + " "
		}
	}

	if len(o) == 0 {
		return o
	}

	return o[:len(o)-1]
}
