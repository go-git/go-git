package capability

import (
	"fmt"
	"os"

	check "gopkg.in/check.v1"
)

var _ = check.Suite(&SuiteCapabilities{})

func (s *SuiteCapabilities) TestDefaultAgent(c *check.C) {
	os.Unsetenv("GO_GIT_USER_AGENT_EXTRA")
	ua := DefaultAgent()
	c.Assert(ua, check.Equals, userAgent)
}

func (s *SuiteCapabilities) TestEnvAgent(c *check.C) {
	os.Setenv("GO_GIT_USER_AGENT_EXTRA", "abc xyz")
	ua := DefaultAgent()
	c.Assert(ua, check.Equals, fmt.Sprintf("%s %s", userAgent, "abc xyz"))
}
