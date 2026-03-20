package transport

import (
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// DiscoverVersion reads the first pktline from the reader to determine the
// protocol version. This is used by the client to determine the protocol
// version of the server.
func DiscoverVersion(r ioutil.ReadPeeker) (protocol.Version, error) {
	ver := protocol.V0
	_, pktb, err := pktline.PeekLine(r)
	if err != nil {
		return ver, err
	}

	pkt := strings.TrimSpace(string(pktb))
	if strings.HasPrefix(pkt, "version ") {
		// Consume the version packet
		if _, _, err := pktline.ReadLine(r); err != nil {
			return ver, err
		}
		if v, _ := protocol.Parse(pkt[8:]); v > ver {
			ver = protocol.Version(v)
		}
	}

	return ver, nil
}

// NegotiateVersion resolves the effective protocol version given a
// client-requested version and the server's allowed set. If the requested
// version is in the allowed set it is returned as-is. Otherwise the highest
// allowed version that is <= the requested version is chosen. If no such
// version exists, the lowest allowed version is returned. A zero mask is
// treated as "all versions allowed".
func NegotiateVersion(requested protocol.Version, allowed protocol.Versions) protocol.Version {
	if allowed == 0 {
		return requested
	}
	if allowed.Has(requested) {
		return requested
	}
	// Highest allowed version <= requested.
	for v := requested - 1; v >= protocol.V0; v-- {
		if allowed.Has(v) {
			return v
		}
	}
	// Fall back to lowest allowed version above requested.
	for v := requested + 1; v <= protocol.V2; v++ {
		if allowed.Has(v) {
			return v
		}
	}
	return requested
}

// ProtocolVersion tries to find the version parameter in the protocol string.
// This expects the protocol string from the GIT_PROTOCOL environment variable.
// This is used by the server to determine the protocol version requested by
// the client.
func ProtocolVersion(p string) protocol.Version {
	var ver protocol.Version
	for param := range strings.SplitSeq(p, ":") {
		if strings.HasPrefix(param, "version=") {
			if v, _ := protocol.Parse(param[8:]); v > ver {
				ver = protocol.Version(v)
			}
		}
	}
	return ver
}
