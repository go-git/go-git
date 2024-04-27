package server

import (
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/protocol"
)

// DetermineProtocolVersion is used to determine the protocol version of the
// server from request parameters.
func DetermineProtocolVersion(params ...string) protocol.Version {
	ver := protocol.VersionV0
	for _, p := range params {
		if strings.HasPrefix(p, "version=") {
			v, _ := strconv.Atoi(p[8:])
			if v := protocol.Version(v); v > ver {
				ver = v
			}
		}
	}
	return ver
}
