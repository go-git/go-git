package capability

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/stretchr/testify/assert"
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

func (s *SuiteCapabilities) TestValidateUnknownCapability() {
	caps := &List{}
	caps.Add("unknown-capability")
	err := Validate(caps)
	s.Error(err)
	s.Contains(err.Error(), "unknown capability")
}

func (s *SuiteCapabilities) TestValidateValidCapabilityNoArgs() {
	caps := &List{}
	caps.Add(ThinPack)
	err := Validate(caps)
	s.NoError(err)
}

func (s *SuiteCapabilities) TestValidateCapabilityWithInvalidArgs() {
	caps := &List{}
	caps.Add(ThinPack, "invalid-arg")
	err := Validate(caps)
	s.ErrorIs(err, ErrArguments)
}

func (s *SuiteCapabilities) TestValidateCapabilityRequiresArgs() {
	caps := &List{}
	caps.Add(SymRef)
	err := Validate(caps)
	s.ErrorIs(err, ErrArgumentsRequired)
}

func (s *SuiteCapabilities) TestValidateCapabilityWithArgs() {
	caps := &List{}
	caps.Add(SymRef, "HEAD:refs/heads/main")
	err := Validate(caps)
	s.NoError(err)
}

func (s *SuiteCapabilities) TestValidateAgentRequiresArg() {
	caps := &List{}
	caps.Add(Agent)
	err := Validate(caps)
	s.ErrorIs(err, ErrArgumentsRequired)
}

func (s *SuiteCapabilities) TestValidateAgentWithArg() {
	caps := &List{}
	caps.Add(Agent, "go-git/6.x")
	err := Validate(caps)
	s.NoError(err)
}

func (s *SuiteCapabilities) TestValidateAgentTooManyArgs() {
	caps := &List{}
	caps.Set(Agent, "go-git/6.x", "extra")
	err := Validate(caps)
	s.ErrorIs(err, ErrMultipleArguments)
}

func (s *SuiteCapabilities) TestValidateObjectFormatOptionalArg() {
	caps := &List{}
	caps.Add(ObjectFormat)
	err := Validate(caps)
	s.Error(err)

	caps.Set(ObjectFormat, "sha256")
	err = Validate(caps)
	s.NoError(err)
}

func (s *SuiteCapabilities) TestValidateObjectFormatTooManyArgs() {
	caps := &List{}
	caps.Set(ObjectFormat, "sha256", "extra")
	err := Validate(caps)
	s.ErrorIs(err, ErrMultipleArguments)
}

func (s *SuiteCapabilities) TestValidateEmptyList() {
	caps := &List{}
	err := Validate(caps)
	s.NoError(err)
}

func (s *SuiteCapabilities) TestValidateMultipleCapabilities() {
	caps := &List{}
	caps.Add(MultiACK)
	caps.Add(ThinPack)
	caps.Add(Agent, "test")
	err := Validate(caps)
	s.NoError(err)
}

func (s *SuiteCapabilities) TestValidateMultipleCapabilitiesOneInvalid() {
	caps := &List{}
	caps.Add(MultiACK)
	caps.Add(ThinPack, "invalid")
	err := Validate(caps)
	s.ErrorIs(err, ErrArguments)
}

func (s *SuiteCapabilities) TestValidateEmptyArgument() {
	caps := &List{}
	caps.Add(Agent, "")
	err := Validate(caps)
	s.ErrorIs(err, ErrEmptyArgument)
}

func (s *SuiteCapabilities) TestValidateSymRefMultipleArgs() {
	caps := &List{}
	caps.Add(SymRef, "HEAD:refs/heads/main", "foo:refs/heads/foo")
	err := Validate(caps)
	s.NoError(err)
}

func (s *SuiteCapabilities) TestIsKnown() {
	s.True(isKnown(ThinPack))
	s.True(isKnown(Agent))
	s.False(isKnown("unknown-cap"))
}

func (s *SuiteCapabilities) TestRequiresArgument() {
	s.True(requiresArgument(Agent))
	s.True(requiresArgument(PushCert))
	s.True(requiresArgument(SymRef))
	s.False(requiresArgument(ThinPack))
	s.False(requiresArgument(MultiACK))
}

func (s *SuiteCapabilities) TestAllowsMultipleArguments() {
	s.True(allowsMultipleArguments(SymRef))
	s.False(allowsMultipleArguments(Agent))
	s.False(allowsMultipleArguments(PushCert))
	s.False(allowsMultipleArguments(ThinPack))
}

func (s *SuiteCapabilities) TestValidateNoEmptyArgs() {
	s.NoError(validateNoEmptyArgs([]string{"a", "b"}))
	s.NoError(validateNoEmptyArgs([]string{}))
	s.NoError(validateNoEmptyArgs(nil))
	s.ErrorIs(validateNoEmptyArgs([]string{"a", "", "c"}), ErrEmptyArgument)
}

func (s *SuiteCapabilities) TestValidateUnknownCapabilityFirst() {
	// Unknown capability should be caught before argument validation
	caps := &List{}
	caps.Add("unknown", "arg")
	err := Validate(caps)
	s.Error(err)
	s.Contains(err.Error(), "unknown capability")
}

func (s *SuiteCapabilities) TestValidateSessionID() {
	// Can fit in a packet line.
	sessionID := "0123456789abcdef0123456789abcdef01234567"
	caps := &List{}
	caps.Add(SessionID, sessionID)
	err := Validate(caps)
	s.NoError(err)

	// Too long for a packet line.
	sessionID = strings.Repeat("a", pktline.MaxPayloadSize+1)
	caps = &List{}
	caps.Add(SessionID, sessionID)
	err = Validate(caps)
	s.ErrorContains(err, "too long")

	// Empty session ID is not allowed.
	caps = &List{}
	caps.Add(SessionID, "")
	err = Validate(caps)
	s.ErrorIs(err, ErrEmptyArgument)

	// Cannot contain spaces.
	caps = &List{}
	caps.Add(SessionID, "invalid session id")
	err = Validate(caps)
	s.ErrorContains(err, "invalid char")
}
