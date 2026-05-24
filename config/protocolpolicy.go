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

// ValidateProtocolPolicy returns nil for "always", "never", "user"
// (case-insensitive) or the empty string; any other value yields
// an error suitable for surfacing at config-load time. The key is
// included in the diagnostic to match canonical Git's
// parse_protocol_config[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/transport.c#L1062-L1072
func ValidateProtocolPolicy(key, value string) error {
	switch strings.ToLower(value) {
	case "", ProtocolNever, ProtocolUser, ProtocolAlways:
		return nil
	}
	return fmt.Errorf("unknown value for %q: %q", key, value)
}
