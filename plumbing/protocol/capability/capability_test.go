package capability

import (
	"fmt"
	"os"
)

func (s *SuiteCapabilities) TestDefaultAgent() {
	os.Unsetenv("GO_GIT_USER_AGENT_EXTRA")
	ua := DefaultAgent()
	s.Equal(userAgent, ua)
}

func (s *SuiteCapabilities) TestEnvAgent() {
	os.Setenv("GO_GIT_USER_AGENT_EXTRA", "abc xyz")
	ua := DefaultAgent()
	s.Equal(fmt.Sprintf("%s %s", userAgent, "abc xyz"), ua)
}
