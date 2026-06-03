package transport

import (
	"errors"
	"os"
	"testing"

	"github.com/go-git/go-git/v6/config"
)

func boolPtr(v bool) *bool { return &v }

// Built-in defaults match canonical Git's get_protocol_config fallback
// table in transport.c.
func TestDefaultProtocolPolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scheme string
		want   string
	}{
		{"http", config.ProtocolAlways},
		{"https", config.ProtocolAlways},
		{"git", config.ProtocolAlways},
		{"ssh", config.ProtocolAlways},
		{"file", config.ProtocolUser},
		{"ext", config.ProtocolNever},
		{"unknown-scheme", config.ProtocolUser},
	}
	for _, tc := range cases {
		t.Run(tc.scheme, func(t *testing.T) {
			t.Parallel()
			if got := DefaultProtocolPolicy(tc.scheme); got != tc.want {
				t.Fatalf("DefaultProtocolPolicy(%q) = %v, want %v", tc.scheme, got, tc.want)
			}
		})
	}
}

// IsProtocolAllowed must follow canonical Git's get_protocol_config +
// is_transport_allowed precedence: GIT_ALLOW_PROTOCOL overrides
// everything; otherwise protocol.<name>.allow beats protocol.allow,
// which beats the built-in default.
func TestIsProtocolAllowed_Matrix(t *testing.T) {
	type tc struct {
		name           string
		envAllowSet    bool   // when false, GIT_ALLOW_PROTOCOL is unset; envAllow ignored
		envAllow       string // value to set when envAllowSet is true
		cfgAllow       string
		cfgNamed       map[string]string
		scheme         string
		fromUser       *bool
		envFromUserSet bool   // when false, GIT_PROTOCOL_FROM_USER is unset; envFromUser ignored
		envFromUser    string // value to set when envFromUserSet is true
		want           bool
	}

	cases := []tc{
		// Built-in defaults, user-initiated (env unset → default true).
		{name: "http default allows", scheme: "http", want: true},
		{name: "https default allows", scheme: "https", want: true},
		{name: "git default allows", scheme: "git", want: true},
		{name: "ssh default allows", scheme: "ssh", want: true},
		{name: "ext default denies", scheme: "ext", want: false},

		// file scheme: user default. User-initiated allowed, non-user denied.
		{name: "file default user-initiated allowed", scheme: "file", fromUser: boolPtr(true), want: true},
		{name: "file default non-user-initiated denied", scheme: "file", fromUser: boolPtr(false), want: false},
		{name: "file default env-user=0 denied", scheme: "file", envFromUserSet: true, envFromUser: "0", want: false},
		{name: "file default env-user=1 allowed", scheme: "file", envFromUserSet: true, envFromUser: "1", want: true},
		{name: "file default env-unset allows", scheme: "file", fromUser: nil, want: true},

		// protocol.<name>.allow overrides built-in.
		{name: "file always via per-scheme cfg", scheme: "file", cfgNamed: map[string]string{"file": config.ProtocolAlways}, fromUser: boolPtr(false), want: true},
		{name: "file never via per-scheme cfg", scheme: "file", cfgNamed: map[string]string{"file": config.ProtocolNever}, fromUser: boolPtr(true), want: false},
		{name: "http never via per-scheme cfg", scheme: "http", cfgNamed: map[string]string{"http": config.ProtocolNever}, want: false},

		// protocol.allow used when per-scheme not set.
		{name: "ssh never via global cfg", scheme: "ssh", cfgAllow: config.ProtocolNever, want: false},
		{name: "file always via global cfg", scheme: "file", cfgAllow: config.ProtocolAlways, fromUser: boolPtr(false), want: true},
		{name: "file user via global cfg", scheme: "file", cfgAllow: config.ProtocolUser, fromUser: boolPtr(false), want: false},

		// per-scheme wins over protocol.allow.
		{name: "per-scheme always beats global never", scheme: "file", cfgAllow: config.ProtocolNever, cfgNamed: map[string]string{"file": config.ProtocolAlways}, want: true},
		{name: "per-scheme never beats global always", scheme: "file", cfgAllow: config.ProtocolAlways, cfgNamed: map[string]string{"file": config.ProtocolNever}, want: false},

		// GIT_ALLOW_PROTOCOL takes absolute precedence.
		{name: "env allow-list permits listed scheme", envAllowSet: true, envAllow: "file:https", scheme: "file", fromUser: boolPtr(false), want: true},
		{name: "env allow-list denies unlisted scheme", envAllowSet: true, envAllow: "http:https", scheme: "file", fromUser: boolPtr(true), want: false},
		{name: "env allow-list overrides per-scheme allow", envAllowSet: true, envAllow: "http", cfgNamed: map[string]string{"file": config.ProtocolAlways}, scheme: "file", want: false},
		{name: "env allow-list overrides per-scheme deny", envAllowSet: true, envAllow: "file", cfgNamed: map[string]string{"file": config.ProtocolNever}, scheme: "file", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envAllowSet {
				t.Setenv("GIT_ALLOW_PROTOCOL", tc.envAllow)
			} else {
				t.Setenv("GIT_ALLOW_PROTOCOL", "")
				_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
			}
			if tc.envFromUserSet {
				t.Setenv("GIT_PROTOCOL_FROM_USER", tc.envFromUser)
			} else {
				t.Setenv("GIT_PROTOCOL_FROM_USER", "")
				_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")
			}

			cfg := config.NewConfig()
			cfg.Protocol.Allow = tc.cfgAllow
			cfg.Protocol.AllowByName = tc.cfgNamed

			got, err := IsProtocolAllowed(cfg, tc.scheme, tc.fromUser)
			if err != nil {
				t.Fatalf("IsProtocolAllowed(%q, fromUser=%v) unexpected err: %v", tc.scheme, tc.fromUser, err)
			}
			if got != tc.want {
				t.Fatalf("IsProtocolAllowed(%q, fromUser=%v) = %v, want %v",
					tc.scheme, tc.fromUser, got, tc.want)
			}
		})
	}
}

// CheckRequest is the gate invoked from Handshake/Connect; it must
// return ErrProtocolNotAllowed when the resolved policy denies the
// scheme, and nil otherwise.
func TestCheckRequest_DefaultDeniesNonUserFile(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	req := mustRequest(t, "file:///tmp/repo")
	req.FromUser = boolPtr(false)

	err := CheckRequest(req)
	if !errors.Is(err, ErrProtocolNotAllowed) {
		t.Fatalf("CheckRequest err = %v, want ErrProtocolNotAllowed", err)
	}
}

func TestCheckRequest_AllowsUserInitiatedFile(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	req := mustRequest(t, "file:///tmp/repo")
	req.FromUser = boolPtr(true)

	if err := CheckRequest(req); err != nil {
		t.Fatalf("CheckRequest err = %v, want nil", err)
	}
}

func TestCheckRequest_HonorsCfgOverride(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	cfg := config.NewConfig()
	cfg.Protocol.AllowByName = map[string]string{"file": config.ProtocolAlways}

	req := mustRequest(t, "file:///tmp/repo")
	req.FromUser = boolPtr(false)
	req.Config = cfg

	if err := CheckRequest(req); err != nil {
		t.Fatalf("CheckRequest err = %v, want nil (file=always)", err)
	}
}

func TestCheckRequest_NilRequest(t *testing.T) {
	t.Parallel()
	if err := CheckRequest(nil); err == nil {
		t.Fatal("CheckRequest(nil) returned nil; want error")
	}
}

// Canonical Git's git_env_bool / git_parse_maybe_bool accept
// "true"/"yes"/"on" → true, "false"/"no"/"off" → false (case-
// insensitive), integers via strconv (zero → false, non-zero →
// true), and empty string → false. Unparseable values die.
//
// Reference: https://github.com/git/git/blob/v2.54.0/parse.c#L157-L200
func TestResolveFromUser_CanonicalGrammar(t *testing.T) {
	cases := []struct {
		name    string
		env     string // empty literal means t.Setenv("", "") — see set below
		set     bool   // whether to call t.Setenv at all
		want    bool
		wantErr bool
	}{
		{name: "unset uses default true", set: false, want: true},
		{name: "empty string is false (canonical)", set: true, env: "", want: false},
		{name: "literal 0 is false", set: true, env: "0", want: false},
		{name: "literal 1 is true", set: true, env: "1", want: true},
		{name: "true is true", set: true, env: "true", want: true},
		{name: "TRUE is true", set: true, env: "TRUE", want: true},
		{name: "yes is true", set: true, env: "yes", want: true},
		{name: "on is true", set: true, env: "on", want: true},
		{name: "false is false", set: true, env: "false", want: false},
		{name: "FALSE is false", set: true, env: "FALSE", want: false},
		{name: "no is false", set: true, env: "no", want: false},
		{name: "off is false", set: true, env: "off", want: false},
		{name: "negative integer is true", set: true, env: "-1", want: true},
		{name: "garbage returns error", set: true, env: "maybe", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("GIT_PROTOCOL_FROM_USER", tc.env)
			} else {
				t.Setenv("GIT_PROTOCOL_FROM_USER", "")
				_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")
			}
			got, err := resolveFromUser(nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// Canonical Git's protocol_allow_list treats `GIT_ALLOW_PROTOCOL=""`
// (set but empty) as an empty allow-list, denying every scheme.
// The unset case falls through to the per-scheme / global / default
// policy resolution.
//
// Reference: https://github.com/git/git/blob/v2.54.0/transport.c#L1037-L1054
func TestIsProtocolAllowed_EmptyAllowListDeniesAll(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	for _, scheme := range []string{"http", "https", "git", "ssh", "file"} { //nolint:paralleltest // mutates process env
		t.Run(scheme, func(t *testing.T) {
			got, err := IsProtocolAllowed(nil, scheme, boolPtr(true))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got {
				t.Fatalf("scheme %q must be denied when GIT_ALLOW_PROTOCOL is empty", scheme)
			}
		})
	}
}

// Canonical's string_list_split keeps empty entries, but
// string_list_has_string only matches non-empty schemes — so leading,
// trailing, or doubled colons are harmless. Pin that contract.
//
// Reference: https://github.com/git/git/blob/v2.54.0/transport.c#L1037-L1054
func TestIsProtocolAllowed_AllowListEdgeCases(t *testing.T) {
	cases := []struct {
		env    string
		scheme string
		want   bool
	}{
		{env: "file:", scheme: "file", want: true},
		{env: "file:", scheme: "", want: false},
		{env: ":file", scheme: "file", want: true},
		{env: "file::https", scheme: "https", want: true},
		{env: "file::https", scheme: "file", want: true},
		{env: "file::https", scheme: "", want: false},
		{env: "file", scheme: " file", want: false}, // no implicit trim
	}
	for _, tc := range cases {
		t.Run(tc.env+"/"+tc.scheme, func(t *testing.T) {
			t.Setenv("GIT_ALLOW_PROTOCOL", tc.env)
			got, err := IsProtocolAllowed(nil, tc.scheme, boolPtr(true))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("scheme %q with env %q: got %v want %v", tc.scheme, tc.env, got, tc.want)
			}
		})
	}
}

func mustRequest(t *testing.T, raw string) *Request {
	t.Helper()
	u, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return &Request{URL: u}
}
