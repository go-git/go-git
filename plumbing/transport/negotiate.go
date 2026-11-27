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

// Pacing of the have rounds, as measured from the git client: how many haves
// a round offers and when the client stops, not which haves they are. The
// first round offers 16 haves. After that the two kinds of connection differ:
//
//   - Over a stateless connection every round is a request of its own that
//     repeats the wants and the common haves, so rounds should be few. The
//     total offered doubles with each round, and from 16384 on grows by a
//     tenth.
//   - Over a stateful connection the server answers on the same pipe or
//     socket while haves are being written, and a round too large to
//     buffer would block both ends. The total doubles once, to 32, and
//     then grows by 32 a round.
//
// After a first acknowledgement the client stops offering haves once more
// than 256 in a row have gone unacknowledged, and accepts a larger pack
// instead. The count is checked between rounds, so the round that crosses
// it is sent whole and "done" follows in a round without new haves.
const (
	firstHaveBatch  = 16
	maxUnackedHaves = 256

	// statefulHaveStep is the most a stateful round adds to the total.
	statefulHaveStep = 32
	// largeHaveTotal is the total from which stateless growth slows down.
	largeHaveTotal = 16384
)

// nextHaveTotal returns how many haves should have been offered by the end
// of the next round, given that total were due by the end of this one.
func nextHaveTotal(statelessRPC bool, total int) int {
	switch {
	case statelessRPC && total < largeHaveTotal, !statelessRPC && total < statefulHaveStep:
		return total * 2
	case statelessRPC:
		return total * 11 / 10
	default:
		return total + statefulHaveStep
	}
}

// haveNegotiator decides which haves go into each round of a v0 fetch, keeps
// what the server has said about them, and knows which of the server's
// answers are still to be read.
type haveNegotiator struct {
	// statelessRPC says that every round is a request of its own, to a
	// server that remembers nothing of the rounds before it.
	statelessRPC bool

	// pending are the haves not offered yet. They are taken from the end.
	pending []plumbing.Hash
	// target is how many haves are to be offered by the end of the next
	// round, and offered how many have been so far.
	target, offered int

	// common holds the haves the server reported as common.
	common map[plumbing.Hash]struct{}
	// repeat lists those haves in the order they were first reported, for
	// a stateless client to send again with every later round.
	repeat []plumbing.Hash

	// anyAcknowledged is set by the first acknowledgement that carries a
	// status. Only from then on do unacknowledged haves count towards
	// giving up: before it, they are all the client has to go on.
	anyAcknowledged bool
	// ready is set once the server has said it knows enough to build a
	// pack.
	ready bool
	// unacknowledged counts the haves offered since the server last
	// reported a have as common for the first time.
	unacknowledged int

	// ahead says that a round has been written on a stateful connection
	// whose answer has not been read yet.
	ahead bool
	// final is set by an acknowledgement without a status before "done" was
	// sent. A server that knows no multi-ack sends one for the first have it
	// shares and nothing more after it, so the client goes straight to
	// "done" and has no further answer to wait for.
	final bool
}

// newHaveNegotiator returns a negotiator that offers haves. It works on a
// copy, so the caller's slice is left as it is.
func newHaveNegotiator(statelessRPC bool, haves []plumbing.Hash) *haveNegotiator {
	return &haveNegotiator{
		statelessRPC: statelessRPC,
		pending:      slices.Clone(haves),
		target:       firstHaveBatch,
		common:       map[plumbing.Hash]struct{}{},
	}
}

// nextRound returns the haves of the next round and whether it carries
// "done".
//
// A round is never cut short. It is the last one when the haves run out
// before it is full, which includes having none left at all. It is also the
// last one, and offers nothing new, once the server has said "ready", has
// sent an acknowledgement without a status, or more than maxUnackedHaves
// haves in a row have gone unacknowledged; these are looked at between
// rounds, so the round that crosses the limit is sent
// whole and "done" follows on its own.
func (n *haveNegotiator) nextRound() (haves []plumbing.Hash, done bool) {
	// A stateless server remembers nothing, so every request repeats the
	// common haves, the last one included: it is the one the pack is built
	// for.
	if n.statelessRPC {
		haves = slices.Clone(n.repeat)
	}

	batch := n.target - n.offered
	if n.ready || n.final || (n.anyAcknowledged && n.unacknowledged > maxUnackedHaves) {
		batch = 0
	}
	fresh := min(batch, len(n.pending))
	for range fresh {
		haves = append(haves, n.pending[len(n.pending)-1])
		n.pending = n.pending[:len(n.pending)-1]
	}
	n.offered += fresh
	n.unacknowledged += fresh
	n.target = nextHaveTotal(n.statelessRPC, n.target)

	return haves, batch == 0 || fresh < batch
}

// answersDue returns how many of the server's answers are to be read after
// the round just written.
//
// A stateless round is answered before the next request can be made. On a
// stateful connection the client stays one round ahead, as git does: the
// answer to a round is read after the next round has been written, so that
// the server always has haves to work on. "done" ends that, and the answer
// still owed is read along with the last one. The connection therefore has
// to hold the answer to one round while the next is being written.
func (n *haveNegotiator) answersDue(done bool) int {
	switch {
	case n.final:
		return 0
	case n.statelessRPC:
		return 1
	case done && n.ahead:
		n.ahead = false
		return 2
	case done:
		return 1
	case n.ahead:
		return 1
	default:
		n.ahead = true
		return 0
	}
}

// record takes in the acknowledgements of one server response.
func (n *haveNegotiator) record(acks []packp.ACK) {
	for _, ack := range acks {
		if ack.Status > 0 {
			n.anyAcknowledged = true
		}

		switch ack.Status {
		case 0:
			n.final = true
		case packp.ACKContinue:
			n.unacknowledged = 0
		case packp.ACKReady:
			n.ready = true
			n.unacknowledged = 0
		case packp.ACKCommon:
			_, known := n.common[ack.Hash]
			n.common[ack.Hash] = struct{}{}
			if !known {
				n.unacknowledged = 0
				if n.statelessRPC {
					n.repeat = append(n.repeat, ack.Hash)
				}
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

	negotiator := newHaveNegotiator(statelessRPC, req.Haves)
	for firstRound := true; ; firstRound = false {
		haves, done := negotiator.nextRound()
		uphav := packp.UploadHaves{Haves: haves, Done: done}

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

		// On a stateful connection the server sends its shallow-update
		// before it reads any haves, so it is read before they are written.
		if !statelessRPC {
			if err := readShallows(statelessRPC, reader, req, &shallowInfo, firstRound); err != nil {
				return nil, err
			}
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
		}

		for range negotiator.answersDue(done) {
			var srvrs packp.ServerResponse
			if err := srvrs.Decode(reader); err != nil {
				return nil, fmt.Errorf("decoding server-response: %w", err)
			}
			negotiator.record(srvrs.ACKs)
		}
		if done {
			break
		}
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

// ReconcileObjectFormatV2 aligns the storer's object format with the Protocol
// v2 server's advertised object-format before any packfile is requested. On a
// fresh clone the storer's format is unset (HEAD still points at the
// refs/heads/.invalid placeholder) and the server's sha256 is adopted;
// otherwise a mismatch is a hard error, since indexing a sha256 pack as sha1
// (or vice versa) corrupts the store and only surfaces later as a checksum
// failure. It mirrors NegotiatePack's v0/v1 object-format handling and git's
// fetch-pack.c, including the case where the server omits object-format (it
// only speaks sha1) but the client repository uses another algorithm.
func ReconcileObjectFormatV2(st storage.Storer, caps capability.List) error {
	var clientFormat config.ObjectFormat
	if cfg, err := st.Config(); err == nil && cfg != nil {
		clientFormat = cfg.Extensions.ObjectFormat
	}

	advertised := caps.Get(capability.ObjectFormat)
	if len(advertised) == 0 {
		// The server advertised no object-format, so it only speaks sha1.
		// Upstream errors when the client repo uses a different algorithm
		// rather than letting it fail later on a checksum mismatch.
		if clientFormat != config.UnsetObjectFormat && clientFormat != config.SHA1 {
			return fmt.Errorf("the server does not support algorithm '%s'", clientFormat)
		}
		return nil
	}

	var serverFormat config.ObjectFormat
	switch v := config.ObjectFormat(advertised[0]); v {
	case config.SHA1, config.SHA256:
		serverFormat = v
	case config.UnsetObjectFormat:
		// An empty value carries no algorithm; treat it exactly as an absent
		// object-format (the server only speaks sha1). Apply the same guard as
		// the len(advertised)==0 branch so a client repo on a different
		// algorithm is rejected rather than slipping past to fail later on a
		// checksum mismatch.
		if clientFormat != config.UnsetObjectFormat && clientFormat != config.SHA1 {
			return fmt.Errorf("the server does not support algorithm '%s'", clientFormat)
		}
		return nil
	default:
		// An algorithm go-git does not speak. Fail fast rather than proceed
		// with the wrong hash format, matching NegotiatePack's v0/v1 handling.
		return fmt.Errorf("server advertised unsupported object-format %q", v)
	}

	// Adopt the server format on a fresh clone: unset client + sha256 server,
	// with HEAD still at the clone placeholder.
	if clientFormat == config.UnsetObjectFormat && serverFormat == config.SHA256 {
		if ref, err := st.Reference(plumbing.HEAD); err == nil && ref.Target().String() == "refs/heads/.invalid" {
			if setter, ok := st.(xstorage.ObjectFormatSetter); ok {
				if err := setter.SetObjectFormat(serverFormat); err != nil {
					return fmt.Errorf("unable to set object format: %w", err)
				}
				clientFormat = serverFormat
			}
		}
	}

	if clientFormat == config.UnsetObjectFormat {
		clientFormat = config.SHA1
	}

	if serverFormat != clientFormat {
		return fmt.Errorf("mismatched algorithms: client %s; server %s", clientFormat, serverFormat)
	}
	return nil
}
