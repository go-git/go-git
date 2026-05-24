package transport

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/config"
)

// ErrProtocolNotAllowed is returned by CheckRequest when the resolved
// protocol policy denies the request's URL scheme.
var ErrProtocolNotAllowed = errors.New("transport: protocol not allowed")

// Env vars consulted by the policy gate. Mirror canonical Git's
// protocol_allow_list and is_transport_allowed in transport.c — see
// the isProtocolAllowed comment for the upstream link.
const (
	envAllowProtocol = "GIT_ALLOW_PROTOCOL"
	envProtocolUser  = "GIT_PROTOCOL_FROM_USER"
)

// defaultProtocolPolicy returns the built-in policy string for a
// scheme when no per-scheme or fallback config is set. Matches the
// fallback table in canonical Git's get_protocol_config.
func defaultProtocolPolicy(scheme string) string {
	switch scheme {
	case "http", "https", "git", "ssh":
		return config.ProtocolAlways
	case "ext":
		return config.ProtocolNever
	}
	return config.ProtocolUser
}

// isProtocolAllowed returns (true, nil) when scheme is permitted,
// (false, nil) when denied by the resolved policy string, and
// (false, err) when the policy configured for scheme, or the
// environment value for GIT_PROTOCOL_FROM_USER, is malformed. As in
// canonical Git, a protocol.allow or protocol.<name>.allow value is
// checked here, when the scheme's policy is looked up, so a bad value
// for one protocol does not affect another.
//
// fromUser carries the user-initiated flag: a non-nil value is taken
// at face value; nil falls back to GIT_PROTOCOL_FROM_USER (default
// true), matching canonical Git's transport_check_allowed.
//
// GIT_ALLOW_PROTOCOL, when set, replaces every other source: the
// scheme is allowed iff it appears in the colon-separated list. An
// empty value denies all schemes, matching canonical Git's
// protocol_allow_list.
//
// [1]: https://github.com/git/git/blob/v2.54.0/transport.c#L1037-L1149
func isProtocolAllowed(cfg *config.Config, scheme string, fromUser *bool) (bool, error) {
	if scheme == "" {
		return false, nil
	}
	if list, ok := allowListFromEnv(); ok {
		return slices.Contains(list, scheme), nil
	}
	policy, err := resolvePolicy(cfg, scheme)
	if err != nil {
		return false, err
	}
	switch policy {
	case config.ProtocolAlways:
		return true, nil
	case config.ProtocolNever:
		return false, nil
	case config.ProtocolUser:
		return resolveFromUser(fromUser)
	}
	return false, nil
}

// CheckRequest gates a Request against the resolved protocol
// policy. Returns ErrProtocolNotAllowed (wrapped with the scheme)
// when denied, or a non-nil error other than ErrProtocolNotAllowed
// when GIT_PROTOCOL_FROM_USER carries a malformed value.
func CheckRequest(req *Request) error {
	if req == nil || req.URL == nil {
		return fmt.Errorf("transport: nil request or URL")
	}
	scheme := req.URL.Scheme
	allowed, err := isProtocolAllowed(req.Config, scheme, req.FromUser)
	if err != nil {
		return fmt.Errorf("transport: %w", err)
	}
	if allowed {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrProtocolNotAllowed, scheme)
}

// resolvePolicy walks protocol.<name>.allow, then protocol.allow,
// then the built-in default, and returns the policy lower-cased, or
// the error for a malformed configured value.
func resolvePolicy(cfg *config.Config, scheme string) (string, error) {
	if cfg != nil {
		policy, err := cfg.ProtocolPolicy(scheme)
		if err != nil || policy != "" {
			return policy, err
		}
	}
	return defaultProtocolPolicy(scheme), nil
}

// allowListFromEnv returns the parsed GIT_ALLOW_PROTOCOL list.
// Distinguishes "unset" from "set but empty": canonical Git's
// protocol_allow_list[1] treats the second case as an empty
// allow-list that denies every scheme.
//
// [1]: https://github.com/git/git/blob/v2.54.0/transport.c#L1037-L1054
func allowListFromEnv() ([]string, bool) {
	v, ok := os.LookupEnv(envAllowProtocol)
	if !ok {
		return nil, false
	}
	if v == "" {
		return nil, true
	}
	return strings.Split(v, ":"), true
}

// resolveFromUser implements canonical Git's GIT_PROTOCOL_FROM_USER
// fallback. An explicit non-nil value wins; nil consults the env,
// defaulting to true when the env is unset. Empty env value is
// false (matching git_env_bool[1]). Unparseable env value returns
// an error so callers see the misconfiguration.
//
// [1]: https://github.com/git/git/blob/v2.54.0/parse.c#L157-L200
func resolveFromUser(fu *bool) (bool, error) {
	if fu != nil {
		return *fu, nil
	}
	v, ok := os.LookupEnv(envProtocolUser)
	if !ok {
		return true, nil
	}
	if v == "" {
		// Canonical git_env_bool parses "" via
		// git_parse_maybe_bool_text as 0 (false).
		return false, nil
	}
	b, ok := parseEnvBool(v)
	if !ok {
		return false, fmt.Errorf("bad boolean value %q for %s", v, envProtocolUser)
	}
	return b, nil
}

// parseEnvBool reads a boolean as canonical Git's git_parse_maybe_bool[1]
// does: true/yes/on and false/no/off case-insensitively, or any decimal
// integer, non-zero meaning true. It reports false for anything else.
//
// [1]: https://github.com/git/git/blob/v2.54.0/parse.c#L157-L182
func parseEnvBool(v string) (value, ok bool) {
	switch strings.ToLower(v) {
	case "true", "yes", "on":
		return true, true
	case "false", "no", "off":
		return false, true
	}
	if i, err := strconv.Atoi(v); err == nil {
		return i != 0, true
	}
	return false, false
}
