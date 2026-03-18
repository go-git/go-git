package capability

import "github.com/go-git/go-git/v6/plumbing/protocol"

// Capabilities provides a version-aware view of server capabilities.
//
// For V0/V1, it wraps a flat capability list from AdvRefs.
// For V2, it wraps per-command capability sets and maps V2 fetch/push
// sub-capabilities to their V0/V1 equivalents for backward-compatible queries.
type Capabilities struct {
	version  protocol.Version
	v1       *List            // V0/V1: flat list from AdvRefs
	commands map[string]*List // V2: per-command capabilities
	global   *List            // V2: top-level capabilities (agent, object-format, etc.)
}

// NewCapabilitiesV1 creates a Capabilities from a V0/V1 flat capability list.
func NewCapabilitiesV1(list *List) *Capabilities {
	if list == nil {
		list = NewList()
	}
	return &Capabilities{
		version: protocol.V0,
		v1:      list,
	}
}

// NewCapabilitiesV2 creates a Capabilities from a V2 server capability
// advertisement, which consists of top-level (global) capabilities and
// per-command capability sets.
func NewCapabilitiesV2(global *List, commands map[string]*List) *Capabilities {
	if global == nil {
		global = NewList()
	}
	if commands == nil {
		commands = make(map[string]*List)
	}
	return &Capabilities{
		version:  protocol.V2,
		global:   global,
		commands: commands,
	}
}

// Version returns the protocol version these capabilities were negotiated with.
func (c *Capabilities) Version() protocol.Version {
	return c.version
}

// Supports returns true if the given capability is supported.
//
// For V0/V1 this checks the flat capability list.
// For V2 this checks global capabilities first, then the "fetch" and "ls-refs"
// command capabilities, mapping V2 sub-capabilities to their V0/V1 equivalents.
func (c *Capabilities) Supports(cap Capability) bool {
	if c.version != protocol.V2 {
		return c.v1.Supports(cap)
	}
	return c.v2Supports(cap)
}

// Get returns the values for a capability.
//
// For V0/V1 this queries the flat capability list.
// For V2 this checks global capabilities first, then command capabilities.
func (c *Capabilities) Get(cap Capability) []string {
	if c.version != protocol.V2 {
		return c.v1.Get(cap)
	}
	return c.v2Get(cap)
}

// SupportsCommand returns whether the V2 server advertised the given command.
// For V0/V1 connections this always returns true, since V0/V1 implicitly
// supports upload-pack and receive-pack.
func (c *Capabilities) SupportsCommand(cmd string) bool {
	if c.version != protocol.V2 {
		return true
	}
	_, ok := c.commands[cmd]
	return ok
}

// CommandCapabilities returns the capability list for a specific V2 command.
// For V0/V1 connections this returns the flat capability list.
func (c *Capabilities) CommandCapabilities(cmd string) *List {
	if c.version != protocol.V2 {
		return c.v1
	}
	if l, ok := c.commands[cmd]; ok {
		return l
	}
	return NewList()
}

// V1List returns the underlying V0/V1 capability list.
// For V2 connections this returns nil.
func (c *Capabilities) V1List() *List {
	if c.version != protocol.V2 {
		return c.v1
	}
	return nil
}

// v2Supports checks V2 capabilities. Global capabilities (agent,
// object-format, server-option) are checked first, then per-command
// capabilities are checked to find V0/V1-equivalent capabilities.
func (c *Capabilities) v2Supports(cap Capability) bool {
	if c.global.Supports(cap) {
		return true
	}

	// V2 fetch sub-capabilities map to many V0/V1 capabilities.
	if fetchCaps, ok := c.commands["fetch"]; ok {
		if fetchCaps.Supports(cap) {
			return true
		}
	}

	// ls-refs capabilities (e.g. symref, unborn).
	if lsRefsCaps, ok := c.commands["ls-refs"]; ok {
		if lsRefsCaps.Supports(cap) {
			return true
		}
	}

	return false
}

// v2Get retrieves capability values from V2 capabilities.
func (c *Capabilities) v2Get(cap Capability) []string {
	if vals := c.global.Get(cap); vals != nil {
		return vals
	}

	if fetchCaps, ok := c.commands["fetch"]; ok {
		if vals := fetchCaps.Get(cap); vals != nil {
			return vals
		}
	}

	if lsRefsCaps, ok := c.commands["ls-refs"]; ok {
		if vals := lsRefsCaps.Get(cap); vals != nil {
			return vals
		}
	}

	return nil
}
