package transport

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

func TestParseURL(t *testing.T) {
	t.Parallel()
	type tt struct {
		input   string
		want    string
		wantErr string
	}

	tests := []tt{
		{
			input: "http://git:pass@github.com:8080/user/repository.git?foo#bar",
			want:  "http://git:pass@github.com:8080/user/repository.git?foo#bar",
		},
		{
			input: "https://git:pass@github.com/user/repository.git?foo#bar",
			want:  "https://git:pass@github.com/user/repository.git?foo#bar",
		},
		{
			input: "http://git:pass@github.com/user/repository.git?foo#bar",
			want:  "http://git:pass@github.com/user/repository.git?foo#bar",
		},
		{
			input: fmt.Sprintf("http://%s:%s@github.com/user/repository.git",
				url.PathEscape("person@mail.com"),
				url.PathEscape(" !\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"),
			),
			want: "http://person%40mail.com:%20%21%22%23$%25&%27%28%29%2A+,-.%2F%3A;%3C=%3E%3F%40%5B%5C%5D%5E_%60%7B%7C%7D~@github.com/user/repository.git",
		},
		{
			input: "http://[::1]:8080/foo.git",
			want:  "http://[::1]:8080/foo.git",
		},
		{
			input: "ssh://git:pass@github.com/user/repository.git?foo#bar",
			want:  "ssh://git:pass@github.com/user/repository.git?foo#bar",
		},
		{
			input: "ssh://git@github.com/user/repository.git",
			want:  "ssh://git@github.com/user/repository.git",
		},
		{
			input: "ssh://github.com/user/repository.git",
			want:  "ssh://github.com/user/repository.git",
		},
		{
			input: "ssh://git@github.com:777/user/repository.git",
			want:  "ssh://git@github.com:777/user/repository.git",
		},
		{
			input: "git@github.com:user/repository.git",
			want:  "ssh://git@github.com/user/repository.git",
		},
		{
			input: "git@github.com:9999/user/repository.git",
			want:  "ssh://git@github.com/9999/user/repository.git",
		},
		{
			// The SCP-like form has no port: everything after the
			// first colon is the path, exactly as canonical Git reads
			// it, so `8080:` here is the start of the path and not a
			// port to dial.
			input: "git@github.com:8080:9999/user/repository.git",
			want:  "ssh://git@github.com/8080:9999/user/repository.git",
		},
		{
			// The user is optional in the SCP-like form. Writing an
			// empty userinfo would render as `ssh://@github.com/...`,
			// a URL that names no user and does not round-trip.
			input: "github.com:user/repository.git",
			want:  "ssh://github.com/user/repository.git",
		},
		{
			input: "[fe80::1]:user/repository.git",
			want:  "ssh://[fe80::1]/user/repository.git",
		},
		{
			// A bracketed literal host, the only way to write an IPv6
			// address in the SCP-like form.
			input: "git@[fe80::1]:user/repository.git",
			want:  "ssh://git@[fe80::1]/user/repository.git",
		},
		{
			input: "git@[fe80::1]:22:user/repository.git",
			want:  "ssh://git@[fe80::1]/22:user/repository.git",
		},
		{
			input: "git://github.com/user/repository.git?foo#bar",
			want:  "git://github.com/user/repository.git?foo#bar",
		},
		{
			// Both halves of the SCP-like form may be empty: the colon
			// is the whole of the syntax. git 2.55 asks `host` for
			// `git-upload-pack ''`.
			input: "host:",
			want:  "ssh://host",
		},
		{
			input: "[fe80::1]:",
			want:  "ssh://[fe80::1]",
		},
		{
			// An empty host, which git 2.55 hands to ssh as the empty
			// string. Note what String() makes of it: net/url has no
			// way to spell an empty host followed by a relative path,
			// so it writes the path where the host goes. The Host and
			// Path fields, which are what the ssh transport reads, are
			// right.
			input: ":path",
			want:  "ssh://path",
		},
		{
			// The scheme ends at the FIRST `://` anywhere, so this is
			// a URL with the unroutable scheme `a`, not SSH to host
			// `a`. git 2.55: fatal: protocol 'a:b' is not supported.
			input: "a:b://c",
			want:  "a:b://c",
		},
		{
			// git 2.55: fatal: protocol 'git@host:a' is not supported.
			input:   "git@host:a://b",
			wantErr: "first path segment in URL cannot contain colon",
		},
		{
			// git 2.55: fatal: protocol '/abs/a' is not supported.
			input:   "/abs/a://b",
			wantErr: "invalid endpoint",
		},
		{
			// git 2.55: fatal: protocol '' is not supported.
			input:   "://a",
			wantErr: "missing protocol scheme",
		},
	}

	// Neither the host nor the path excludes a byte, because Git
	// restricts neither: the whitespace and backslash rules go-git used
	// to apply did not reject these endpoints, they reclassified them
	// as local paths.
	tests = append(tests, []tt{{
		// git 2.55: HOST=[ho st] CMD=[git-upload-pack 'path'].
		input: "ho st:path",
		want:  "ssh://ho%20st/path",
	}, {
		// git 2.55: HOST=[host] CMD=[git-upload-pack '\\path'].
		input: "host:\\path",
		want:  "ssh://host/%5Cpath",
	}, {
		// git 2.55: HOST=[a b] CMD=[git-upload-pack 'c'].
		input: "[a b]:c",
		want:  "ssh://[a%20b]/c",
	}}...)

	if runtime.GOOS != "windows" {
		// A DOS drive prefix is only local on Windows, in go-git as in
		// Git. git 2.55 on macOS: HOST=[C] CMD=[git-upload-pack
		// '\\path\\to\\repo'].
		tests = append(tests, tt{
			input: "C:\\path\\to\\repo",
			want:  "ssh://C/%5Cpath%5Cto%5Crepo",
		})
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			ep, err := ParseURL(tc.input)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, ep.String())
			}
		})
	}
}

func TestParseURLFile(t *testing.T) {
	t.Parallel()
	type tt struct {
		input   string
		want    string
		wantErr string
	}

	tests := []tt{
		{
			input: "/foo.git",
			want:  "file:///foo.git",
		},
		{
			input: "foo.git",
			want:  "file://foo.git",
		},
		{
			input: "file:///foo.git",
			want:  "file:///foo.git",
		},
		{
			input: "file:///path/to/repo",
			want:  "file:///path/to/repo",
		},
		{
			input: "file://C:/path/to/repo",
			want:  "file://C:/path/to/repo",
		},
		{
			input: "file://C:\\path\\to\\repo",
			want:  "file://C:\\path\\to\\repo",
		},
		{
			input:   "http://\\",
			wantErr: "invalid character",
		},
	}

	// A DOS drive prefix is a local path on Windows and an SSH endpoint
	// naming the host `C` everywhere else, because that is what
	// canonical Git does: has_dos_drive_prefix is a no-op off Windows
	// (git-compat-util.h), and url_is_local_not_ssh only reaches the
	// local reading through it. git 2.55 on macOS asks HOST=[C] for
	// CMD=[git-upload-pack '\\foo.git'].
	if runtime.GOOS == "windows" {
		tests = append(tests, []tt{{
			input: "C:\\foo.git",
			want:  "file://C:\\foo.git",
		}, {
			input: "C:\\\\foo.git",
			want:  "file://C:\\\\foo.git",
		}, {
			input: "file:///C:/path/to/repo",
			want:  "file://C:/path/to/repo",
		}, {
			input: "file:///c:\\foo.git",
			want:  "file://c:\\foo.git",
		}}...)
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			ep, err := ParseURL(tc.input)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, strings.TrimPrefix(tc.want, "file://"), ep.Path)
			}
		})
	}
}

func TestFilterUnsupportedCapabilities(t *testing.T) {
	t.Parallel()
	l := &capability.List{}
	l.Set(capability.MultiACK)
	l.Set(capability.MultiACKDetailed)

	assert.False(t, l.Supports(capability.ThinPack))
}

func FuzzParseURL(f *testing.F) {
	f.Fuzz(func(_ *testing.T, input string) {
		ParseURL(input)
	})
}
