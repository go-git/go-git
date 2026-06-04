package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

const (
	initialFlush  = 16
	pipeSafeFlush = 32
	largeFlush    = 16384
	maxInVein     = 256
)

func nextFlush(statelessRPC bool, count int) int {
	if statelessRPC {
		if count < largeFlush {
			return count << 1
		}
		return count * 11 / 10
	}

	if count < pipeSafeFlush {
		return count << 1
	}

	return count + pipeSafeFlush
}

func applyServerACKs(
	statelessRPC bool,
	acks []packp.ACK,
	common map[plumbing.Hash]struct{},
	statelessCommon *[]plumbing.Hash,
	gotContinue *bool,
	gotReady *bool,
	inVein *int,
) {
	for _, ack := range acks {
		if !*gotContinue && ack.Status > 0 {
			*gotContinue = true
		}

		switch ack.Status {
		case packp.ACKContinue:
			*inVein = 0
		case packp.ACKReady:
			*gotReady = true
			*inVein = 0
		case packp.ACKCommon:
			_, alreadyCommon := common[ack.Hash]
			common[ack.Hash] = struct{}{}
			if statelessRPC && !alreadyCommon {
				*statelessCommon = append(*statelessCommon, ack.Hash)
				*inVein = 0
			}
		}
	}
}

// NegotiatePack performs the pack negotiation phase of the fetch operation.
func NegotiatePack(
	ctx context.Context,
	st storage.Storer,
	caps capability.List,
	statelessRPC bool,
	reader io.Reader,
	writer io.WriteCloser,
	req *FetchRequest,
) (shallowInfo *packp.ShallowUpdate, err error) {
	reader = ioutil.NewContextReader(ctx, reader)
	writer = ioutil.NewContextWriteCloser(ctx, writer)

	upreq := &packp.UploadRequest{}
	multiAck := caps.Supports(capability.MultiACK)
	multiAckDetailed := caps.Supports(capability.MultiACKDetailed)
	if multiAckDetailed {
		upreq.Capabilities.Set(capability.MultiACKDetailed)
	} else if multiAck {
		upreq.Capabilities.Set(capability.MultiACK)
	}

	if req.Progress != nil {
		if caps.Supports(capability.Sideband64k) {
			upreq.Capabilities.Set(capability.Sideband64k)
		} else if caps.Supports(capability.Sideband) {
			upreq.Capabilities.Set(capability.Sideband)
		}
	} else if caps.Supports(capability.NoProgress) {
		upreq.Capabilities.Set(capability.NoProgress)
	}

	if caps.Supports(capability.ObjectFormat) {
		var clientFormat, serverFormat config.ObjectFormat
		if capValues := caps.Get(capability.ObjectFormat); len(capValues) > 0 {
			of := config.ObjectFormat(capValues[0])
			switch of {
			case config.SHA1, config.SHA256:
				serverFormat = of
			}
		}

		cfg, err := st.Config()
		if err == nil {
			clientFormat = cfg.Extensions.ObjectFormat
		}

		if clientFormat == config.UnsetObjectFormat && serverFormat == config.SHA256 {
			ref, err := st.Reference(plumbing.HEAD)
			if err == nil && ref.Target().String() == "refs/heads/.invalid" {
				if setter, ok := st.(xstorage.ObjectFormatSetter); ok {
					err := setter.SetObjectFormat(serverFormat)
					if err != nil {
						return nil, fmt.Errorf("unable to set object format: %w", err)
					}
					clientFormat = serverFormat
				}
			}
		}

		if clientFormat == config.UnsetObjectFormat {
			clientFormat = config.SHA1
		}

		if serverFormat != clientFormat {
			return nil, fmt.Errorf("mismatched algorithms: client %s; server %s", clientFormat, serverFormat)
		}

		upreq.Capabilities.Set(capability.ObjectFormat, clientFormat.String())
	}

	if caps.Supports(capability.OFSDelta) {
		upreq.Capabilities.Set(capability.OFSDelta)
	}

	if caps.Supports(capability.Agent) {
		upreq.Capabilities.Set(capability.Agent, capability.DefaultAgent())
	}

	if req.IncludeTags && caps.Supports(capability.IncludeTag) {
		upreq.Capabilities.Set(capability.IncludeTag)
	}

	if req.Filter != "" {
		if caps.Supports(capability.Filter) {
			upreq.Filter = req.Filter
			upreq.Capabilities.Set(capability.Filter)
		} else {
			return nil, ErrFilterNotSupported
		}
	}

	upreq.Wants = req.Wants

	if req.Depth > 0 {
		if !caps.Supports(capability.Shallow) {
			return nil, ErrShallowNotSupported
		}
		upreq.Depth = packp.DepthRequest{Deepen: req.Depth}
		upreq.Shallows, err = st.Shallow()
		if err != nil {
			return nil, err
		}
	}

	if isSubset(req.Wants, req.Haves) && len(upreq.Shallows) == 0 {
		if err := pktline.WriteFlush(writer); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("closing writer: %w", err)
		}
		return nil, ErrNoChange
	}

	common := map[plumbing.Hash]struct{}{}
	var statelessCommon []plumbing.Hash
	var inVein int
	var done bool
	var gotContinue bool
	var gotReady bool
	var sendDoneAfterReady bool
	firstRound := true
	flushAt := initialFlush

	for !done {
		var uphav packp.UploadHaves
		if statelessRPC && !sendDoneAfterReady {
			uphav.Haves = append(uphav.Haves, statelessCommon...)
		}

		batchSize := 0
		if !sendDoneAfterReady {
			batchSize = flushAt
			if gotContinue {
				remaining := maxInVein - inVein
				if remaining <= 0 {
					batchSize = 0
				} else if batchSize > remaining {
					batchSize = remaining
				}
			}
		}

		for i := 0; i < batchSize && len(req.Haves) > 0; i++ {
			uphav.Haves = append(uphav.Haves, req.Haves[len(req.Haves)-1])
			req.Haves = req.Haves[:len(req.Haves)-1]
			inVein++
		}

		done = sendDoneAfterReady || len(req.Haves) == 0 || (gotContinue && inVein >= maxInVein)
		uphav.Done = done

		if isSubset(req.Wants, uphav.Haves) && len(upreq.Shallows) == 0 {
			if err := pktline.WriteFlush(writer); err != nil {
				return nil, err
			}
			if err := writer.Close(); err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("closing writer: %w", err)
			}
			return nil, ErrNoChange
		}

		if firstRound || statelessRPC {
			if err := upreq.Encode(writer); err != nil {
				return nil, fmt.Errorf("sending upload-request: %w", err)
			}
		}

		readc := make(chan error)
		if !statelessRPC {
			go func() { readc <- readShallows(statelessRPC, reader, req, &shallowInfo, firstRound) }()
		}

		if err := uphav.Encode(writer); err != nil {
			return nil, fmt.Errorf("sending upload-haves: %w", err)
		}

		if statelessRPC {
			if err := writer.Close(); err != nil {
				return nil, fmt.Errorf("closing writer: %w", err)
			}
			if err := readShallows(statelessRPC, reader, req, &shallowInfo, firstRound); err != nil {
				return nil, err
			}
		} else {
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
				applyServerACKs(statelessRPC, srvrs.ACKs, common, &statelessCommon, &gotContinue, &gotReady, &inVein)
			}
			readc <- nil
		}()

		if err := <-readc; err != nil {
			return nil, err
		}
		if sendDoneAfterReady {
			break
		}
		if gotReady {
			sendDoneAfterReady = true
		}

		firstRound = false
		flushAt = nextFlush(statelessRPC, flushAt)
	}

	if !statelessRPC {
		if err := writer.Close(); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("closing writer: %w", err)
		}
	}

	return shallowInfo, nil
}

func isSubset(needle, haystack []plumbing.Hash) bool {
	for _, h := range needle {
		if !slices.Contains(haystack, h) {
			return false
		}
	}
	return true
}

func readShallows(
	statelessRPC bool,
	r io.Reader,
	req *FetchRequest,
	shallowInfo **packp.ShallowUpdate,
	firstRound bool,
) error {
	if (firstRound || statelessRPC) && req.Depth > 0 {
		var shupd packp.ShallowUpdate
		if err := shupd.Decode(r); err != nil {
			return fmt.Errorf("decoding shallow-update: %w", err)
		}
		if *shallowInfo == nil {
			*shallowInfo = &shupd
		}
	}
	return nil
}
