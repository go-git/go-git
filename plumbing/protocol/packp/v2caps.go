package packp

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

// V2ServerCapabilities represents the capability advertisement sent by a
// Git protocol V2 server. Unlike V0/V1, which advertises capabilities
// alongside refs, V2 advertises a set of supported commands and global
// capabilities. Each command may have its own sub-capabilities.
//
// Wire format (after the "version 2\n" line has been consumed):
//
//	<capability>\n
//	<capability>=<value>\n
//	...
//	0000
//
// Commands that support sub-capabilities encode them as space-separated
// values:
//
//	fetch=shallow wait-for-done filter
//	ls-refs=unborn
type V2ServerCapabilities struct {
	// Global holds top-level capabilities that are not commands,
	// such as agent, object-format, and server-option.
	Global *capability.List

	// Commands maps command names to their sub-capabilities.
	// For example: "fetch" -> List{shallow, filter, wait-for-done}
	Commands map[string]*capability.List
}

// knownV2Commands lists the well-known V2 commands. Lines matching
// these are parsed as commands; everything else goes into Global.
var knownV2Commands = map[string]bool{
	"ls-refs":     true,
	"fetch":       true,
	"object-info": true,
}

// NewV2ServerCapabilities creates a new, empty V2ServerCapabilities.
func NewV2ServerCapabilities() *V2ServerCapabilities {
	return &V2ServerCapabilities{
		Global:   capability.NewList(),
		Commands: make(map[string]*capability.List),
	}
}

// Decode reads a V2 capability advertisement from r.
// The "version 2\n" pkt-line must have already been consumed.
func (c *V2ServerCapabilities) Decode(r io.Reader) error {
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return fmt.Errorf("reading V2 capability: %w", err)
		}

		if l == pktline.Flush {
			return nil
		}

		text := string(bytes.TrimSuffix(line, eol))
		if text == "" {
			continue
		}

		key, value, hasValue := strings.Cut(text, "=")

		if knownV2Commands[key] {
			list := capability.NewList()
			if hasValue {
				// Sub-capabilities are space-separated.
				for sub := range strings.SplitSeq(value, " ") {
					if sub == "" {
						continue
					}
					// Sub-capabilities are treated as unknown (custom)
					// capabilities so we avoid argument validation.
					if err := list.Add(capability.Capability(sub)); err != nil {
						return fmt.Errorf("adding sub-capability %q for command %q: %w", sub, key, err)
					}
				}
			}
			c.Commands[key] = list
		} else {
			cap := capability.Capability(key)
			if hasValue {
				if err := c.Global.Add(cap, value); err != nil {
					return fmt.Errorf("adding global capability %q: %w", key, err)
				}
			} else {
				if err := c.Global.Add(cap); err != nil {
					return fmt.Errorf("adding global capability %q: %w", key, err)
				}
			}
		}
	}
}

// Encode writes the V2 capability advertisement to w.
// It does NOT write the "version 2\n" prefix.
func (c *V2ServerCapabilities) Encode(w io.Writer) error {
	// Write global capabilities first.
	for _, cap := range c.Global.All() {
		vals := c.Global.Get(cap)
		if len(vals) > 0 {
			for _, v := range vals {
				if _, err := pktline.Writef(w, "%s=%s\n", cap, v); err != nil {
					return err
				}
			}
		} else {
			if _, err := pktline.Writef(w, "%s\n", cap); err != nil {
				return err
			}
		}
	}

	// Write commands with sub-capabilities.
	for name, list := range c.Commands {
		subcaps := list.All()
		if len(subcaps) > 0 {
			parts := make([]string, len(subcaps))
			for i, sc := range subcaps {
				parts[i] = sc.String()
			}
			if _, err := pktline.Writef(w, "%s=%s\n", name, strings.Join(parts, " ")); err != nil {
				return err
			}
		} else {
			if _, err := pktline.Writef(w, "%s\n", name); err != nil {
				return err
			}
		}
	}

	return pktline.WriteFlush(w)
}

// ToCapabilities converts the V2ServerCapabilities into the version-aware
// Capabilities type for use with the Connection interface.
func (c *V2ServerCapabilities) ToCapabilities() *capability.Capabilities {
	return capability.NewCapabilitiesV2(c.Global, c.Commands)
}
