package transport_test

import (
	"fmt"
	"net/url"
	"runtime"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEndpoint(t *testing.T) {
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
			input: "https://git:pass@github.com:443/user/repository.git?foo#bar",
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
			want: "http://person@mail.com:%20%21%22%23$%25&%27%28%29%2A+%2C-.%2F:%3B%3C=%3E%3F@%5B%5C%5D%5E_%60%7B%7C%7D~@github.com/user/repository.git",
		},
		{
			input: "http://[::1]:8080/foo.git",
			want:  "http://[::1]:8080/foo.git",
		},
		{
			input: "ssh://git:pass@github.com:22/user/repository.git?foo#bar",
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
			input: "git@github.com:8080:9999/user/repository.git",
			want:  "ssh://git@github.com:8080/9999/user/repository.git",
		},
		{
			input: "git://github.com:9418/user/repository.git?foo#bar",
			want:  "git://github.com/user/repository.git?foo#bar",
		},
		{
			input: "/foo.git",
			want:  "file:///foo.git",
		},
		{
			input: "foo.git",
			want:  "file://foo.git",
		},
		{
			input: "C:\\foo.git",
			want:  "file://C:\\foo.git",
		},
		{
			input: "C:\\\\foo.git",
			want:  "file://C:\\\\foo.git",
		},
		{
			input: "file:///foo.git",
			want:  "file:///foo.git",
		}, {
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

	if runtime.GOOS == "windows" {
		tests = append(tests, []tt{{
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

			ep, err := transport.NewEndpoint(tc.input)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, ep.String())
			}
		})
	}
}

func TestFilterUnsupportedCapabilities(t *testing.T) {
	l := capability.NewList()
	l.Set(capability.MultiACK)
	l.Set(capability.MultiACKDetailed)

	assert.False(t, l.Supports(capability.ThinPack))
}

func FuzzNewEndpoint(f *testing.F) {
	f.Fuzz(func(_ *testing.T, input string) {
		transport.NewEndpoint(input)
	})
}
