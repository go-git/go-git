package config

import (
	"fmt"
	"strings"
)

// Canonical protocol policy values for `protocol.allow` and
// `protocol.<name>.allow`, matching canonical Git's
// enum protocol_allow_config[1]. The on-disk form is
// case-insensitive; these constants give callers the canonical
// lowercase spelling without forcing them through a typed enum.
// The empty string means "use the resolution chain" (per-scheme
// → global → built-in default).
//
// [1]: https://github.com/git/git/blob/v2.54.0/transport.c#L1056-L1072
const (
	ProtocolNever  = "never"
	ProtocolUser   = "user"
	ProtocolAlways = "always"
)

// ProtocolPolicy returns the policy configured for scheme: the value of
// protocol.<scheme>.allow if it is set, else of protocol.allow, lower-cased,
// or "" when neither is set and the built-in default applies. A set value
// that is not "always", "never" or "user", an empty one included, is an
// error naming its key, as in canonical Git's get_protocol_config[1], which
// parses only the key it looks up.
//
// [1]: https://github.com/git/git/blob/v2.54.0/transport.c#L1075-L1112
func (c *Config) ProtocolPolicy(scheme string) (string, error) {
	if v, ok := c.Protocol.AllowByName[scheme]; ok {
		return parseProtocolPolicy("protocol."+scheme+".allow", v)
	}
	if c.Protocol.Allow != nil {
		return parseProtocolPolicy("protocol.allow", *c.Protocol.Allow)
	}
	return "", nil
}

// parseProtocolPolicy returns value lower-cased if it is a known policy,
// and an error naming key otherwise, with canonical Git's message[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/transport.c#L1062-L1073
func parseProtocolPolicy(key, value string) (string, error) {
	switch v := strings.ToLower(value); v {
	case ProtocolNever, ProtocolUser, ProtocolAlways:
		return v, nil
	}
	return "", fmt.Errorf("unknown value for config '%s': %s", key, value)
}
