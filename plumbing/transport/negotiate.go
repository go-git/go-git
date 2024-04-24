package transport

import (
	"bytes"
	"fmt"
	"io"
	"log"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage"
)

// NegotiatePack returns the result of the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
// TODO: make this generic for both pack-protocol and http-protocol.
func NegotiatePack(
	st storage.Storer,
	conn Connection,
	writer io.WriteCloser,
	reader io.Reader,
	req *FetchRequest,
) (shupd packp.ShallowUpdate, err error) {
	if len(req.Wants) == 0 {
		return shupd, fmt.Errorf("no wants specified")
	}

	caps := conn.Capabilities()

	// Create upload-request
	upreq := packp.NewUploadRequest()
	if caps.Supports(capability.MultiACKDetailed) {
		upreq.Capabilities.Set(capability.MultiACKDetailed) // nolint: errcheck
	} else if caps.Supports(capability.MultiACK) {
		upreq.Capabilities.Set(capability.MultiACK) // nolint: errcheck
	}

	if req.Progress != nil {
		if caps.Supports(capability.Sideband64k) {
			upreq.Capabilities.Set(capability.Sideband64k) // nolint: errcheck
		} else if caps.Supports(capability.Sideband) {
			upreq.Capabilities.Set(capability.Sideband) // nolint: errcheck
		}
	} else if caps.Supports(capability.NoProgress) {
		upreq.Capabilities.Set(capability.NoProgress) // nolint: errcheck
	}

	if caps.Supports(capability.ThinPack) {
		upreq.Capabilities.Set(capability.ThinPack) // nolint: errcheck
	}

	if caps.Supports(capability.OFSDelta) {
		upreq.Capabilities.Set(capability.OFSDelta) // nolint: errcheck
	}

	if caps.Supports(capability.Agent) {
		upreq.Capabilities.Set(capability.Agent, capability.DefaultAgent()) // nolint: errcheck
	}

	if req.IncludeTags && caps.Supports(capability.IncludeTag) {
		upreq.Capabilities.Set(capability.IncludeTag) // nolint: errcheck
	}

	upreq.Wants = req.Wants

	if req.Depth != 0 {
		upreq.Depth = packp.DepthCommits(req.Depth)
		upreq.Capabilities.Set(capability.Shallow) // nolint: errcheck
		upreq.Shallows, err = st.Shallow()
		if err != nil {
			return shupd, err
		}
	}

	// XXX: empty request means haves are a subset of wants, in that case we have
	// everything we asked for. Close the connection and return nil.
	if isSubset(req.Wants, req.Haves) && len(upreq.Shallows) == 0 {
		log.Printf("no need to fetch, we have everything")
		return shupd, pktline.WriteFlush(writer)
	}

	// Create upload-haves
	var uphav packp.UploadHaves
	uphav.Haves = req.Haves

	var (
		done  bool
		srvrs packp.ServerResponse
	)
	isMultiAck := caps.Supports(capability.MultiACK) ||
		caps.Supports(capability.MultiACKDetailed)

	if err := upreq.Validate(); err != nil {
		return shupd, err
	}

	for !done {
		if err := upreq.Encode(writer); err != nil {
			return shupd, fmt.Errorf("sending upload-request: %s", err)
		}

		// Encode upload-haves
		// TODO: support multi_ack and multi_ack_detailed caps
		if err := uphav.Encode(writer); err != nil {
			return shupd, fmt.Errorf("sending upload-haves: %s", err)
		}

		if conn.IsStatelessRPC() && len(uphav.Haves) > 0 {
			if err := pktline.WriteFlush(writer); err != nil {
				return shupd, fmt.Errorf("sending flush-pkt after haves: %s", err)
			}
		}

		// Let the server know we're done
		if _, err := pktline.Writeln(writer, "done"); err != nil {
			return shupd, fmt.Errorf("sending done: %s", err)
		}

		// Close the writer to signal the end of the request
		if err := writer.Close(); err != nil {
			return shupd, fmt.Errorf("closing writer: %s", err)
		}

		// TODO: handle server-response to support incremental fetch i.e.
		// multi_ack and multi_ack_detailed modes.

		done = true

		log.Printf("done sending upload request")

		log.Printf("waiting for server-response")

		// Decode shallow-update
		// If depth is not zero, then we expect a shallow update from the
		// server.
		if req.Depth != 0 {
			if err := shupd.Decode(reader); err != nil {
				return shupd, fmt.Errorf("decoding shallow-update: %s", err)
			}
		}

		// XXX: The server replies with one last NAK/ACK after the client is
		// done sending the request.
		var acks bytes.Buffer
		tee := io.TeeReader(reader, &acks)
		if _, _, err := pktline.ReadLine(tee); err != nil {
			return shupd, fmt.Errorf("reading server-response: %s", err)
		}

		// Decode server-response final ACK/NAK
		if err := srvrs.Decode(&acks, isMultiAck); err != nil {
			return shupd, fmt.Errorf("decoding server-response: %s", err)
		}

		log.Printf("server-response: %s", srvrs)
	}

	return shupd, nil
}

func isSubset(needle []plumbing.Hash, haystack []plumbing.Hash) bool {
	for _, h := range needle {
		found := false
		for _, oh := range haystack {
			if h == oh {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}
