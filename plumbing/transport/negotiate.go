package transport

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
)

// NegotiatePack returns the result of the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
func NegotiatePack(
	ctx context.Context,
	st storage.Storer,
	conn Connection,
	reader io.Reader,
	writer io.WriteCloser,
	req *FetchRequest,
) (shallowInfo *packp.ShallowUpdate, err error) {
	caps := conn.Capabilities()

	// Create upload-request
	upreq := packp.NewUploadRequest()
	multiAck := caps.Supports(capability.MultiACK)
	multiAckDetailed := caps.Supports(capability.MultiACKDetailed)
	if multiAckDetailed {
		upreq.Capabilities.Set(capability.MultiACKDetailed) // nolint: errcheck
	} else if multiAck {
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

	// TODO: support thin-pack
	// if caps.Supports(capability.ThinPack) {
	// 	upreq.Capabilities.Set(capability.ThinPack) // nolint: errcheck
	// }

	if caps.Supports(capability.OFSDelta) {
		upreq.Capabilities.Set(capability.OFSDelta) // nolint: errcheck
	}

	if caps.Supports(capability.Agent) {
		upreq.Capabilities.Set(capability.Agent, capability.DefaultAgent()) // nolint: errcheck
	}

	if req.IncludeTags && caps.Supports(capability.IncludeTag) {
		upreq.Capabilities.Set(capability.IncludeTag) // nolint: errcheck
	}

	// TODO: Support filter

	upreq.Wants = req.Wants

	if req.Depth > 0 {
		if !caps.Supports(capability.Shallow) {
			return nil, fmt.Errorf("server doesn't support shallow clients")
		}

		upreq.Depth = packp.DepthCommits(req.Depth)
		upreq.Shallows, err = st.Shallow()
		if err != nil {
			return nil, err
		}
	}

	// Note: empty request means haves are a subset of wants, in that case we have
	// everything we asked for. Close the connection and return nil.
	if isSubset(req.Wants, req.Haves) && len(upreq.Shallows) == 0 {
		if err := pktline.WriteFlush(writer); err != nil {
			return nil, err
		}

		// Close the writer to signal the end of the request
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("closing writer: %s", err)
		}

		return nil, ErrNoChange
	}

	// Create upload-haves
	common := map[plumbing.Hash]struct{}{}

	var inVein int
	var done bool
	var gotContinue bool // whether we got a continue from the server
	firstRound := true
	for !done {
		// Pop the last 32 or depth have commits from the pending list and
		// insert their parents into the pending list.
		// TODO: Properly build and implement haves negotiation, and move it
		// from remote.go to this package.
		var uphav packp.UploadHaves
		for i := 0; i < 32 && len(req.Haves) > 0; i++ {
			uphav.Haves = append(uphav.Haves, req.Haves[len(req.Haves)-1])
			req.Haves = req.Haves[:len(req.Haves)-1]
			inVein++
		}

		// Let the server know we're done
		const maxInVein = 256
		done = len(req.Haves) == 0 || (gotContinue && inVein >= maxInVein)
		uphav.Done = done

		// Note: empty request means haves are a subset of wants, in that case we have
		// everything we asked for. Close the connection and return nil.
		if isSubset(req.Wants, uphav.Haves) && len(upreq.Shallows) == 0 {
			if err := pktline.WriteFlush(writer); err != nil {
				return nil, err
			}

			// Close the writer to signal the end of the request
			if err := writer.Close(); err != nil {
				return nil, fmt.Errorf("closing writer: %s", err)
			}

			return nil, ErrNoChange
		}

		// Begin the upload-pack negotiation
		if firstRound || conn.StatelessRPC() {
			if err := upreq.Encode(writer); err != nil {
				return nil, fmt.Errorf("sending upload-request: %w", err)
			}
		}

		readc := make(chan error)
		if !conn.StatelessRPC() {
			go func() { readc <- readShallows(conn, reader, req, &shallowInfo, firstRound) }()
		}

		// Encode upload-haves
		if err := uphav.Encode(writer); err != nil {
			return nil, fmt.Errorf("sending upload-haves: %w", err)
		}

		// Close the writer to signal the end of the request
		if conn.StatelessRPC() {
			if err := writer.Close(); err != nil {
				return nil, fmt.Errorf("closing writer: %w", err)
			}

			if err := readShallows(conn, reader, req, &shallowInfo, firstRound); err != nil {
				return nil, err
			}
		} else {
			// Wait for the read channel to be closed
			if err := <-readc; err != nil {
				return nil, err
			}
		}

		go func() {
			defer close(readc)

			if done || len(uphav.Haves) > 0 {
				var srvrs packp.ServerResponse
				if err := srvrs.Decode(reader); err != nil {
					readc <- fmt.Errorf("decoding server-response: %w", err)
					return
				}

				for _, ack := range srvrs.ACKs {
					if !gotContinue && ack.Status > 0 {
						gotContinue = true
					}
					if ack.Status == packp.ACKCommon {
						common[ack.Hash] = struct{}{}
					}
				}
			}

			readc <- nil
		}()

		// Wait for the read channel to be closed
		if err := <-readc; err != nil {
			return nil, err
		}

		firstRound = false
	}

	if !conn.StatelessRPC() {
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("closing writer: %w", err)
		}
	}

	return shallowInfo, nil
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

func readShallows(
	conn Connection,
	r io.Reader,
	req *FetchRequest,
	shallowInfo **packp.ShallowUpdate,
	firstRound bool,
) error {
	// Decode shallow-update
	// If depth is not zero, then we expect a shallow update from the
	// server.
	if (firstRound || conn.StatelessRPC()) && req.Depth > 0 {
		var shupd packp.ShallowUpdate
		if err := shupd.Decode(r); err != nil {
			return fmt.Errorf("decoding shallow-update: %w", err)
		}

		// Only return the first shallow update
		if shallowInfo == nil {
			shallowInfo = new(*packp.ShallowUpdate)
			*shallowInfo = &shupd
		}
	}

	return nil
}
