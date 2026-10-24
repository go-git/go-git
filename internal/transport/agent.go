package transport

import (
	"strings"

	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// AgentCapability returns the value to send as the agent capability for
// userAgent, or for capability.DefaultAgent() when userAgent is empty.
//
// The capability only allows printable ASCII other than space, so userAgent is
// trimmed and every other byte is replaced with '.', as git does in
// redact_non_printables (version.c). The HTTP User-Agent header has no such
// restriction and carries userAgent unchanged, as it does in git.
func AgentCapability(userAgent string) string {
	if userAgent == "" {
		userAgent = capability.DefaultAgent()
	}

	b := []byte(strings.Trim(userAgent, " \t\n\r"))
	for i, c := range b {
		if c <= ' ' || c > '~' {
			b[i] = '.'
		}
	}
	return string(b)
}
