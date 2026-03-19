package transport

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

// NegotiatePackV2 implements the V2 fetch command negotiation.
// Unlike V0/V1 which uses wants/haves over multiple round-trips with
// UploadRequest/UploadHaves, V2 uses a single "command=fetch" per round.
//
// See https://git-scm.com/docs/protocol-v2#_fetch
func NegotiatePackV2(
	ctx context.Context,
	st storage.Storer,
	conn Connection,
	reader io.Reader,
	writer io.WriteCloser,
	req *FetchRequest,
) (resp *packp.V2FetchResponse, err error) {
	reader = ioutil.NewContextReader(ctx, reader)
	writer = ioutil.NewContextWriteCloser(ctx, writer)
	caps := conn.Capabilities()

	// Note: empty request means haves are a subset of wants, in that case we have
	// everything we asked for.
	if isSubset(req.Wants, req.Haves) {
		return nil, ErrNoChange
	}

	// Determine object format.
	objectFormat, err := negotiateObjectFormat(st, caps)
	if err != nil {
		return nil, err
	}

	// Build the V2 fetch request.
	haves := make([]plumbing.Hash, len(req.Haves))
	copy(haves, req.Haves)

	var done bool
	for !done {
		// Pop up to 32 haves per round.
		var roundHaves []plumbing.Hash
		for i := 0; i < 32 && len(haves) > 0; i++ {
			roundHaves = append(roundHaves, haves[len(haves)-1])
			haves = haves[:len(haves)-1]
		}

		done = len(haves) == 0

		fetchReq := &packp.V2FetchRequest{
			Wants:        req.Wants,
			WantRefs:     req.WantRefs,
			Haves:        roundHaves,
			Done:         done,
			OFSDelta:     caps.Supports(capability.OFSDelta),
			ObjectFormat: objectFormat,
		}

		if req.IncludeTags && caps.Supports(capability.IncludeTag) {
			fetchReq.IncludeTag = true
		}

		if req.Progress == nil {
			fetchReq.NoProgress = true
		}

		if caps.Supports(capability.WaitForDone) {
			fetchReq.WaitForDone = true
		}

		if caps.Supports(capability.SidebandAll) {
			fetchReq.SidebandAll = true
		}

		if req.Filter != "" {
			if !caps.Supports(capability.Filter) {
				return nil, ErrFilterNotSupported
			}
			fetchReq.Filter = req.Filter
		}

		if req.Depth > 0 {
			if !caps.Supports(capability.Shallow) {
				return nil, ErrShallowNotSupported
			}
			fetchReq.Depth = req.Depth

			shallows, err := st.Shallow()
			if err != nil {
				return nil, err
			}
			fetchReq.Shallows = shallows
		}

		if err := fetchReq.Encode(writer); err != nil {
			return nil, fmt.Errorf("encoding V2 fetch request: %w", err)
		}

		// For stateless connections (HTTP), close the writer to send the request.
		if conn.StatelessRPC() {
			if err := writer.Close(); err != nil {
				return nil, fmt.Errorf("closing writer: %w", err)
			}
		}

		// Read the response.
		resp = packp.NewV2FetchResponse()
		if err := resp.Decode(reader); err != nil {
			return nil, fmt.Errorf("decoding V2 fetch response: %w", err)
		}

		// If the server sent a packfile, we're done regardless.
		if resp.Packfile != nil {
			return resp, nil
		}

		// If the server indicated "ready", the next response will have
		// the packfile. Send done if we haven't already.
		if resp.Ready && !done {
			done = true
			// Loop once more with done=true.
			continue
		}
	}

	return resp, nil
}

// negotiateObjectFormat determines the object format to use for the
// V2 fetch, matching client and server formats.
func negotiateObjectFormat(st storage.Storer, caps *capability.Capabilities) (string, error) {
	if !caps.Supports(capability.ObjectFormat) {
		return "", nil
	}

	var serverFormat config.ObjectFormat
	if capVals := caps.Get(capability.ObjectFormat); len(capVals) > 0 {
		of := config.ObjectFormat(capVals[0])
		switch of {
		case config.SHA1, config.SHA256:
			serverFormat = of
		}
	}

	var clientFormat config.ObjectFormat
	cfg, err := st.Config()
	if err == nil && cfg != nil {
		clientFormat = cfg.Extensions.ObjectFormat
	}

	// Handle clone: the storage may not have an object format set yet.
	if clientFormat == config.UnsetObjectFormat && serverFormat == config.SHA256 {
		ref, err := st.Reference(plumbing.HEAD)
		if err == nil && ref.Target().String() == "refs/heads/.invalid" {
			if setter, ok := st.(xstorage.ObjectFormatSetter); ok {
				if err := setter.SetObjectFormat(serverFormat); err != nil {
					return "", fmt.Errorf("unable to set object format: %w", err)
				}
				clientFormat = serverFormat
			}
		}
	}

	if clientFormat == config.UnsetObjectFormat {
		clientFormat = config.SHA1
	}

	if serverFormat != clientFormat {
		return "", fmt.Errorf("mismatched algorithms: client %s; server %s", clientFormat, serverFormat)
	}

	return clientFormat.String(), nil
}
