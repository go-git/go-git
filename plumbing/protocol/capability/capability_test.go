package capability

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func TestDefaultAgent(t *testing.T) {
	t.Setenv("GO_GIT_USER_AGENT_EXTRA", "")
	ua := DefaultAgent()
	assert.Equal(t, userAgent, ua)
}

func TestEnvAgent(t *testing.T) {
	t.Setenv("GO_GIT_USER_AGENT_EXTRA", "abc xyz")
	ua := DefaultAgent()
	assert.Equal(t, fmt.Sprintf("%s %s", userAgent, "abc xyz"), ua)
}

func (s *SuiteCapabilities) TestValidate() {
	cases := []struct {
		name    string
		add     [][]string
		set     [][]string
		wantErr error
		errStr  string
	}{
		{
			name:    "empty list",
			wantErr: nil,
		},
		{
			name: "unknown capability",
			add: [][]string{
				{"unknown-capability"},
			},
			errStr: "unknown capability",
		},
		{
			name: "unknown capability before argument validation",
			add: [][]string{
				{"unknown", "arg"},
			},
			errStr: "unknown capability",
		},
		{
			name: "valid capability no args",
			add: [][]string{
				{ThinPack},
			},
			wantErr: nil,
		},
		{
			name: "capability with invalid args",
			add: [][]string{
				{ThinPack, "invalid-arg"},
			},
			wantErr: ErrArguments,
		},
		{
			name: "capability requires args",
			add: [][]string{
				{SymRef},
			},
			wantErr: ErrArgumentsRequired,
		},
		{
			name: "capability with args",
			add: [][]string{
				{SymRef, "HEAD:refs/heads/main"},
			},
			wantErr: nil,
		},
		{
			name: "agent requires arg",
			add: [][]string{
				{Agent},
			},
			wantErr: ErrArgumentsRequired,
		},
		{
			name: "agent with arg",
			add: [][]string{
				{Agent, "go-git/6.x"},
			},
			wantErr: nil,
		},
		{
			name: "agent too many args",
			set: [][]string{
				{Agent, "go-git/6.x", "extra"},
			},
			wantErr: ErrMultipleArguments,
		},
		{
			name: "object-format requires arg",
			add: [][]string{
				{ObjectFormat},
			},
			errStr: "requires an argument",
		},
		{
			name: "object-format with arg",
			set: [][]string{
				{ObjectFormat, "sha256"},
			},
			wantErr: nil,
		},
		{
			name: "object-format too many args",
			set: [][]string{
				{ObjectFormat, "sha256", "extra"},
			},
			wantErr: ErrMultipleArguments,
		},
		{
			name: "multiple valid capabilities",
			add: [][]string{
				{MultiACK},
				{ThinPack},
				{Agent, "test"},
			},
			wantErr: nil,
		},
		{
			name: "multiple capabilities one invalid",
			add: [][]string{
				{MultiACK},
				{ThinPack, "invalid"},
			},
			wantErr: ErrArguments,
		},
		{
			name: "empty argument",
			add: [][]string{
				{Agent, ""},
			},
			wantErr: ErrEmptyArgument,
		},
		{
			name: "symref multiple args allowed",
			add: [][]string{
				{SymRef, "HEAD:refs/heads/main", "foo:refs/heads/foo"},
			},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			caps := &List{}
			for _, args := range tc.add {
				caps.Add(args[0], args[1:]...)
			}
			for _, args := range tc.set {
				caps.Set(args[0], args[1:]...)
			}

			err := Validate(caps)
			switch {
			case tc.wantErr != nil:
				s.ErrorIs(err, tc.wantErr)
			case tc.errStr != "":
				s.Error(err)
				s.Contains(err.Error(), tc.errStr)
			default:
				s.NoError(err)
			}
		})
	}
}

func (s *SuiteCapabilities) TestValidateSessionID() {
	cases := []struct {
		name    string
		args    []string
		wantErr error
		errStr  string
	}{
		{
			name: "valid session id",
			args: []string{"0123456789abcdef0123456789abcdef01234567"},
		},
		{
			name:   "too long for packet line",
			args:   []string{strings.Repeat("a", pktline.MaxPayloadSize+1)},
			errStr: "too long",
		},
		{
			name:    "empty session id",
			args:    []string{""},
			wantErr: ErrEmptyArgument,
		},
		{
			name:   "contains spaces",
			args:   []string{"invalid session id"},
			errStr: "invalid char",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			caps := &List{}
			caps.Add(SessionID, tc.args...)
			err := Validate(caps)
			switch {
			case tc.wantErr != nil:
				s.ErrorIs(err, tc.wantErr)
			case tc.errStr != "":
				s.ErrorContains(err, tc.errStr)
			default:
				s.NoError(err)
			}
		})
	}
}

func (s *SuiteCapabilities) TestIsKnown() {
	cases := []struct {
		name string
		cap  Capability
		want bool
	}{
		{name: "known ThinPack", cap: ThinPack, want: true},
		{name: "known Agent", cap: Agent, want: true},
		{name: "unknown", cap: "unknown-cap", want: false},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, isKnown(tc.cap))
		})
	}
}

func (s *SuiteCapabilities) TestRequiresArgument() {
	cases := []struct {
		name string
		cap  Capability
		want bool
	}{
		{name: "Agent", cap: Agent, want: true},
		{name: "PushCert", cap: PushCert, want: true},
		{name: "SymRef", cap: SymRef, want: true},
		{name: "ThinPack", cap: ThinPack, want: false},
		{name: "MultiACK", cap: MultiACK, want: false},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, requiresArgument(tc.cap))
		})
	}
}

func (s *SuiteCapabilities) TestAllowsMultipleArguments() {
	cases := []struct {
		name string
		cap  Capability
		want bool
	}{
		{name: "SymRef", cap: SymRef, want: true},
		{name: "Agent", cap: Agent, want: false},
		{name: "PushCert", cap: PushCert, want: false},
		{name: "ThinPack", cap: ThinPack, want: false},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, allowsMultipleArguments(tc.cap))
		})
	}
}

func (s *SuiteCapabilities) TestValidateNoEmptyArgs() {
	cases := []struct {
		name    string
		args    []string
		wantErr error
	}{
		{name: "non-empty args", args: []string{"a", "b"}},
		{name: "empty slice", args: []string{}},
		{name: "nil slice", args: nil},
		{name: "contains empty", args: []string{"a", "", "c"}, wantErr: ErrEmptyArgument},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			err := validateNoEmptyArgs(tc.args)
			if tc.wantErr != nil {
				s.ErrorIs(err, tc.wantErr)
			} else {
				s.NoError(err)
			}
		})
	}
}
