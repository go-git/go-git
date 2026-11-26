package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ErrNoChange is returned by FetchV2 when every wanted object is already present
// in the client's haves and no shallow change was requested, so that there is
// nothing to ask the server for. transport.ErrNoChange aliases this value.
var ErrNoChange = errors.New("no change")

// Pacing of the have rounds, as measured from the git client (how many
// haves a round offers, not which haves they are): 16 new haves in the first
// round and twice as many in each round after it. Once a round has reached
// 16384 haves, the next is only a tenth larger, so that requests from a
// client that shares little with the server stop doubling.
// After a first acknowledgement the client stops offering haves when 256
// in a row have gone unacknowledged, and accepts a larger pack instead.
const (
	firstHaveBatch  = 16
	maxUnackedHaves = 256

	// largeHaveBatch is the batch size from which growth slows down.
	largeHaveBatch = 16384
)

// nextHaveBatch returns the size of the batch that follows one of count.
func nextHaveBatch(count int) int {
	if count < largeHaveBatch {
		return count * 2
	}
	return count * 11 / 10
}

// haveNegotiator decides which haves go into each round of a v2 fetch, and
// keeps what the server has acknowledged.
//
// A v2 fetch command stands on its own: the server remembers nothing from
// one round to the next, so every round repeats the haves acknowledged so
// far and adds a batch of new ones.
type haveNegotiator struct {
	// pending are the haves not offered yet. They are taken from the end.
	pending []plumbing.Hash
	// batch is the number of new haves the next round offers.
	batch int

	// common holds the haves the server reported as common.
	common map[plumbing.Hash]struct{}
	// repeat lists those haves in the order they were first reported, to
	// send again with every later round.
	repeat []plumbing.Hash
	// anyAcknowledged is set by the first acknowledgement. Only from then
	// on do unacknowledged haves count towards giving up.
	anyAcknowledged bool
	// unacknowledged counts the haves offered since the last
	// acknowledgement.
	unacknowledged int
}

// newHaveNegotiator returns a negotiator that offers haves. It works on a
// copy, so the caller's slice is left as it is.
func newHaveNegotiator(haves []plumbing.Hash) *haveNegotiator {
	return &haveNegotiator{
		pending: slices.Clone(haves),
		batch:   firstHaveBatch,
		common:  make(map[plumbing.Hash]struct{}),
	}
}

// nextRound returns the haves of the next round: the common ones again,
// then a new batch. done reports whether the round is the last, because
// no have was left to offer or because too many in a row went
// unacknowledged.
func (n *haveNegotiator) nextRound() (haves []plumbing.Hash, done bool) {
	fresh := min(n.batch, len(n.pending))
	haves = slices.Clone(n.repeat)
	for range fresh {
		haves = append(haves, n.pending[len(n.pending)-1])
		n.pending = n.pending[:len(n.pending)-1]
	}
	n.unacknowledged += fresh
	n.batch = nextHaveBatch(n.batch)

	done = fresh == 0 || (n.anyAcknowledged && n.unacknowledged >= maxUnackedHaves)
	return haves, done
}

// acknowledge takes in the haves the server reported as common in its
// answer to a round.
func (n *haveNegotiator) acknowledge(acks []plumbing.Hash) {
	for _, ack := range acks {
		if _, ok := n.common[ack]; !ok {
			n.common[ack] = struct{}{}
			n.repeat = append(n.repeat, ack)
		}
		n.anyAcknowledged = true
		n.unacknowledged = 0
	}
}

// CommandFunc runs a single Protocol v2 command: it encodes req into the
// request and decodes the response via resp. A session's Command method
// satisfies this signature, so the shared v2 helpers stay decoupled from the
// public transport.Commander interface (and the import cycle it would create).
type CommandFunc func(ctx context.Context, cmd string, req packp.CommandArgs, resp packp.Decoder) error

// ClientCapabilities returns the capabilities a v2 client sends with each
// command: the agent and the server's object-format echoed back so both sides
// agree on the hash algorithm. server is the capability advertisement the
// server sent during the handshake.
//
// Both are gated on the server having advertised them: upstream git only sends
// agent when the server advertised agent, and object-format when the server
// advertised object-format (fetch-pack.c). This keeps the client a conformant
// peer that never sends a capability the server did not offer.
func ClientCapabilities(server capability.List) capability.List {
	var caps capability.List
	if agent := server.Get(capability.Agent); len(agent) > 0 {
		caps.Set(capability.Agent, capability.DefaultAgent())
	}
	if of := server.Get(capability.ObjectFormat); len(of) > 0 {
		caps.Set(capability.ObjectFormat, of[0])
	}
	return caps
}

// FetchSupports reports whether the server advertised the given fetch feature
// in its v2 capability advertisement (fetch=<feature>...).
func FetchSupports(server capability.List, feature string) bool {
	for _, v := range server.Get(capability.FetchCmd) {
		if slices.Contains(strings.Fields(v), feature) {
			return true
		}
	}
	return false
}

// lsRefsSupportsUnborn reports whether the server advertised the ls-refs
// "unborn" feature (ls-refs=unborn).
func lsRefsSupportsUnborn(server capability.List) bool {
	return slices.Contains(server.Get(capability.LsRefs), "unborn")
}

// LsRefs lists references using the v2 ls-refs command run through cmd. It
// always requests peeled tags and symref targets so HEAD resolves to its
// branch, and requests unborn HEAD reporting when the server advertises it. An
// unborn HEAD is returned as a symbolic reference and named by UnbornHead.
func LsRefs(ctx context.Context, cmd CommandFunc, server capability.List, refPrefixes []string) (*packp.LsRefsOutput, error) {
	args := &packp.LsRefsArgs{
		Peel:        true,
		Symrefs:     true,
		Unborn:      lsRefsSupportsUnborn(server),
		RefPrefixes: refPrefixes,
	}

	out := &packp.LsRefsOutput{}
	if err := cmd(ctx, "ls-refs", args, out); err != nil {
		return nil, err
	}

	return out, nil
}

// IsEmptyLsRefs reports whether out advertises no reference with an object id,
// the v2 equivalent of an empty v0/v1 advertisement. An unborn HEAD is not a
// reference (connect.c process_ref_v2), and the decoder keeps it in References
// once at most. A symbolic HEAD that carries an object id is a reference, even
// when a ref-prefix left its target out of the result.
func IsEmptyLsRefs(out *packp.LsRefsOutput) bool {
	n := len(out.References)
	if out.UnbornHead != "" {
		n--
	}
	return n == 0
}

// wantsLocal reports whether every wanted object is already present in haves, so
// the fetch has nothing to retrieve.
func wantsLocal(wants, haves []plumbing.Hash) bool {
	if len(wants) == 0 {
		return false
	}
	have := make(map[plumbing.Hash]struct{}, len(haves))
	for _, h := range haves {
		have[h] = struct{}{}
	}
	for _, w := range wants {
		if _, ok := have[w]; !ok {
			return false
		}
	}
	return true
}

// FetchRound runs a single fetch command round. When the returned output has
// Packfile set, packReader is positioned at the first packfile pkt-line so the
// caller can stream it. If packReader implements io.Closer, Fetch closes it
// once the round is done (each HTTP round owns a response body; a stream
// transport returns its persistent reader, which is not a Closer).
type FetchRound func(args *packp.FetchArgs) (out *packp.FetchOutput, packReader io.Reader, err error)

// FetchV2 drives the v2 fetch negotiation and streams the resulting packfile into
// st. Each round sends the wants, the common commits acknowledged so far, and
// a growing batch of haves, until the server reports "ready" (the packfile
// follows in the same response) or the client stops offering haves and sends
// "done". The packfile (always sideband-64k muxed in v2) is streamed here, and
// any shallow-info from the response is applied to st.
//
// The caller is responsible for validating optional features against the server
// advertisement (see FetchSupports) before requesting Filter or Depth.
func FetchV2(ctx context.Context, st storage.Storer, req *FetchRequest, round FetchRound) error {
	// Everything wanted is already local and no shallow change was requested,
	// so there is nothing to negotiate.
	if req.Depth == 0 && wantsLocal(req.Wants, req.Haves) {
		return ErrNoChange
	}

	baseArgs := &packp.FetchArgs{
		Wants:      req.Wants,
		OFSDelta:   true,
		NoProgress: req.Progress == nil,
		IncludeTag: req.IncludeTags,
	}
	if req.Filter != "" {
		baseArgs.Filter = req.Filter
	}
	if req.Depth > 0 {
		baseArgs.Deepen = req.Depth
		shallows, err := st.Shallow()
		if err != nil {
			return err
		}
		baseArgs.Shallows = shallows
	}

	negotiator := newHaveNegotiator(req.Haves)

	var shallowInfo *packp.ShallowUpdate
	for {
		args := *baseArgs
		args.Haves, args.Done = negotiator.nextRound()

		out, packReader, err := round(&args)
		if err != nil {
			return err
		}

		if out.ShallowInfo != nil {
			shallowInfo = &packp.ShallowUpdate{
				Shallows:   out.ShallowInfo.Shallows,
				Unshallows: out.ShallowInfo.Unshallows,
			}
			// A bad shallow line is refused before the pack is read, so a
			// rejected update stores nothing. The fetch fails, so the
			// response is closed without draining it for reuse.
			if err := CheckShallowUpdate(st, shallowInfo); err != nil {
				if c, ok := packReader.(io.Closer); ok {
					_ = c.Close()
				}
				return err
			}
		}

		if out.Acknowledgments != nil {
			negotiator.acknowledge(out.Acknowledgments.ACKs)
		}

		if out.Packfile {
			streamErr := streamPackfile(ctx, st, packReader, req.Progress, req.Filter)
			// streamPackfile wraps packReader in a NewContextReader, so
			// whether this may be closed is that wrapper's question to answer.
			if ioutil.ReadFinished(ctx, streamErr) {
				closeReader(packReader)
			}
			if streamErr != nil {
				return streamErr
			}
			break
		}

		closeReader(packReader)
		if args.Done {
			return fmt.Errorf("transport: server sent no packfile after done")
		}
	}

	if shallowInfo != nil {
		if err := UpdateShallow(st, shallowInfo); err != nil {
			return err
		}
	}

	return nil
}

// streamPackfile demultiplexes the sideband-64k packfile stream into st.
//
// A non-empty filter means the server withheld the objects it matched, so the
// pack is recorded as coming from a promisor remote. Git reads unmarked
// absences as corruption: fsck reports broken links to them and gc fails.
func streamPackfile(ctx context.Context, st storage.Storer, packReader io.Reader, progress sideband.Progress, filter packp.Filter) error {
	reader := ioutil.NewContextReader(ctx, packReader)
	demuxer := sideband.NewDemuxer(sideband.Sideband64k, reader)
	if progress != nil {
		demuxer.Progress = progress
	}
	if filter != "" {
		// The marker is left empty. Git fills it with the refs it sought on
		// this path and leaves it empty when repacking, and accepts either,
		// because only the file's presence is ever consulted.
		if err := packfile.UpdatePromisorObjectStorage(st, demuxer, ""); err != nil {
			return err
		}
	} else if err := packfile.UpdateObjectStorage(st, demuxer); err != nil {
		return err
	}

	// Storage that parses the pack stops reading at its trailer, so read the
	// rest of the section here: an error the server sends after the pack
	// must fail the fetch, as must pack data after it, trailing progress
	// must reach the caller, and a stream session must be left after the
	// flush-pkt for its next command. The packfile section is always muxed,
	// and the demuxer stops at its closing flush-pkt.
	return DrainPackSection(demuxer)
}

// closeReader drains and closes r when it owns a closable resource (such as an
// HTTP response body). Draining any unread bytes (e.g. the v2 response-end
// pkt-line) before Close lets net/http reuse the connection across negotiation
// rounds. Persistent stream readers do not implement io.Closer and are left
// open for the next round.
func closeReader(r io.Reader) {
	if c, ok := r.(io.Closer); ok {
		_, _ = io.Copy(io.Discard, r)
		_ = c.Close()
	}
}

// CheckShallowUpdate returns an error when info holds an object ID that is
// not of st's object format. Call it before the pack is read, so that a
// rejected fetch leaves storage as it was.
func CheckShallowUpdate(st storage.Storer, info *packp.ShallowUpdate) error {
	cfg, err := st.Config()
	if err != nil {
		return err
	}
	format := formatcfg.DefaultObjectFormat
	if cfg != nil && cfg.Extensions.ObjectFormat != formatcfg.UnsetObjectFormat {
		format = cfg.Extensions.ObjectFormat
	}
	for _, hashes := range [][]plumbing.Hash{info.Shallows, info.Unshallows} {
		for _, h := range hashes {
			if h.HexSize() != format.HexSize() {
				return fmt.Errorf("shallow update: %s is not a %s object ID", h, format)
			}
		}
	}
	return nil
}

// UpdateShallow merges a shallow update into st's shallow boundary. The
// update must have passed [CheckShallowUpdate].
func UpdateShallow(st storage.Storer, info *packp.ShallowUpdate) error {
	shallows, err := st.Shallow()
	if err != nil {
		return err
	}

outer:
	for _, s := range info.Shallows {
		for _, old := range shallows {
			if s == old {
				continue outer
			}
		}
		shallows = append(shallows, s)
	}

	for _, s := range info.Unshallows {
		for i, old := range shallows {
			if s == old {
				shallows = append(shallows[:i], shallows[i+1:]...)
				break
			}
		}
	}

	return st.SetShallow(shallows)
}
