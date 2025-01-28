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
		pktline.ReadLine(r) // nolint:errcheck
		if v, _ := protocol.Parse(pkt[8:]); v > ver {
			ver = protocol.Version(v)
		}
	}

	return ver, nil
}

// ProtocolVersion tries to find the version parameter in the protocol string.
// This expects the protocol string from the GIT_PROTOCOL environment variable.
// This is used by the server to determine the protocol version requested by
// the client.
func ProtocolVersion(p string) protocol.Version {
	var ver protocol.Version
	for _, param := range strings.Split(p, ":") {
		if strings.HasPrefix(param, "version=") {
			if v, _ := protocol.Parse(param[8:]); v > ver {
				ver = protocol.Version(v)
			}
		}
	}
	return ver
}
