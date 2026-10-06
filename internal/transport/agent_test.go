package transport

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		userAgent string
		want      string
	}{
		{userAgent: "gitlab-sync/1.2.3", want: "gitlab-sync/1.2.3"},
		{userAgent: "my app/1.0", want: "my.app/1.0"},
		{userAgent: " \tmy-app/1.0\r\n", want: "my-app/1.0"},
		{userAgent: "my-app/1.0\x00\x7f", want: "my-app/1.0.."},
		{userAgent: " my app/1.0\té ", want: "my.app/1.0..."},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, AgentCapability(tt.userAgent), "user agent %q", tt.userAgent)
	}
}

func TestAgentCapabilityDefault(t *testing.T) {
	t.Setenv("GO_GIT_USER_AGENT_EXTRA", "myapp/1.0")

	assert.Equal(t, "go-git/6.x.myapp/1.0", AgentCapability(""))
}
