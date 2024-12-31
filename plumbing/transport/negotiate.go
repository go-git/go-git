package transport

import (
	"fmt"
	"io"
	"sort"

	"github.com/go-git/go-git/v5/internal/reference"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage"
)

// NegotiatePack returns the result of the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
func NegotiatePack(
	st storage.Storer,
	conn Connection,
	reader io.Reader,
	writer io.WriteCloser,
	req *FetchRequest,
) (shallows []plumbing.Hash, err error) {
	if len(req.Wants) == 0 {
		return nil, fmt.Errorf("no wants specified")
	}

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
	localRefs, err := reference.References(st)
	if err != nil {
		return nil, fmt.Errorf("getting local references: %s", err)
	}

	var pending []*object.Commit
	for _, r := range localRefs {
		c, err := object.GetCommit(st, r.Hash())
		if err == nil {
			pending = append(pending, c)
		}
	}

	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Committer.When.Before(pending[j].Committer.When)
	})

	var inVein int
	var done bool
	var gotContinue bool // whether we got a continue from the server
	firstRound := true
	for !done {
		if firstRound || conn.StatelessRPC() {
			firstRound = false
			if err := upreq.Encode(writer); err != nil {
				return nil, fmt.Errorf("sending upload-request: %s", err)
			}
		}

		// Pop the last 32 have commits from the pending list and insert their
		// parents into the pending list.
		var uphav packp.UploadHaves
		uphav.Haves = []plumbing.Hash{}
		for i := 0; i < 32 && len(pending) > 0; i++ {
			c := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			parents := c.Parents()
			if err := parents.ForEach(func(p *object.Commit) error {
				pending = append(pending, p)
				return nil
			}); err != nil {
				return nil, fmt.Errorf("getting parents: %s", err)
			}

			uphav.Haves = append(uphav.Haves, c.Hash)
			inVein++
		}

		// Let the server know we're done
		const maxInVein = 256
		done = len(pending) == 0 || (gotContinue && inVein >= maxInVein)
		uphav.Done = done

		// Encode upload-haves
		if err := uphav.Encode(writer); err != nil {
			return nil, fmt.Errorf("sending upload-haves: %w", err)
		}

		// Close the writer to signal the end of the request
		if conn.StatelessRPC() {
			if err := writer.Close(); err != nil {
				return nil, fmt.Errorf("closing writer: %s", err)
			}
		}

		if done || len(uphav.Haves) > 0 {
			var srvrs packp.ServerResponse
			if err := srvrs.Decode(reader); err != nil {
				return nil, fmt.Errorf("decoding server-response: %s", err)
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
	}

	if !conn.StatelessRPC() {
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("closing writer: %w", err)
		}
	}

	// Decode shallow-update
	// If depth is not zero, then we expect a shallow update from the
	// server.
	var shupd packp.ShallowUpdate
	if req.Depth != 0 {
		if err := shupd.Decode(reader); err != nil {
			return nil, fmt.Errorf("decoding shallow-update: %s", err)
		}
	}

	return shupd.Shallows, nil
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
