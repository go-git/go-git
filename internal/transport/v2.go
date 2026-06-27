package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ErrNoChange is returned by FetchV2 when every wanted object is already present
// in the client's haves and no shallow change was requested, mirroring git's
// everything_local short-circuit in do_fetch_pack_v2. transport.ErrNoChange
// aliases this value.
var ErrNoChange = errors.New("no change")

// Negotiation pacing, mirroring git's fetch-pack.c (INITIAL_FLUSH, LARGE_FLUSH,
// MAX_IN_VAIN). v2 is stateless per command, so the stateless schedule applies.
const (
	initialFlush = 16
	largeFlush   = 16384
	maxInVain    = 256
)

func nextFlush(count int) int {
	if count < largeFlush {
		return count << 1
	}
	return count * 11 / 10
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
// branch, and requests unborn HEAD reporting when the server advertises it. The
// returned references include a symbolic HEAD (and an unborn HEAD as a symref
// whose target has no hash reference) so callers can detect an unborn branch.
func LsRefs(ctx context.Context, cmd CommandFunc, server capability.List, refPrefixes []string) ([]*plumbing.Reference, error) {
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

	return out.References, nil
}

// HasHashRef reports whether refs contains at least one hash (non-symbolic)
// reference. A v2 ls-refs result with only a symbolic or unborn HEAD and no
// hash references corresponds to an empty repository, so callers treat the
// absence of hash references the same as the v0/v1 empty advertisement.
func HasHashRef(refs []*plumbing.Reference) bool {
	for _, r := range refs {
		if r.Type() == plumbing.HashReference {
			return true
		}
	}
	return false
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
// st. It mirrors git's do_fetch_pack_v2: each round sends the wants, the common
// commits acked so far, and a growing batch of haves, until the server reports
// "ready" (the packfile follows in the same response) or the client runs out of
// haves and sends "done". The packfile (always sideband-64k muxed in v2) is
// streamed here, and any shallow-info from the response is applied to st.
//
// The caller is responsible for validating optional features against the server
// advertisement (see FetchSupports) before requesting Filter or Depth.
func FetchV2(ctx context.Context, st storage.Storer, req *FetchRequest, round FetchRound) error {
	// Everything wanted is already local and no shallow change was requested:
	// short-circuit before opening negotiation, matching git's everything_local.
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

	// Pop haves from a private copy so the caller's slice is left untouched.
	remaining := append([]plumbing.Hash(nil), req.Haves...)
	var common []plumbing.Hash
	seen := make(map[plumbing.Hash]struct{})
	havesToSend := initialFlush
	inVain := 0
	seenAck := false

	var shallowInfo *packp.ShallowUpdate
	for {
		args := *baseArgs
		// v2 fetch is stateless per command: re-send every common commit acked
		// so far, then a fresh batch of haves.
		roundHaves := append([]plumbing.Hash(nil), common...)
		batch := 0
		for batch < havesToSend && len(remaining) > 0 {
			roundHaves = append(roundHaves, remaining[len(remaining)-1])
			remaining = remaining[:len(remaining)-1]
			batch++
			inVain++
		}
		havesToSend = nextFlush(havesToSend)
		args.Haves = roundHaves
		args.Done = batch == 0 || (seenAck && inVain >= maxInVain)

		out, packReader, err := round(&args)
		if err != nil {
			return err
		}

		if out.ShallowInfo != nil {
			shallowInfo = &packp.ShallowUpdate{
				Shallows:   out.ShallowInfo.Shallows,
				Unshallows: out.ShallowInfo.Unshallows,
			}
		}

		if out.Acknowledgments != nil {
			for _, ack := range out.Acknowledgments.ACKs {
				if _, ok := seen[ack]; !ok {
					seen[ack] = struct{}{}
					common = append(common, ack)
				}
				seenAck = true
				inVain = 0
			}
		}

		if out.Packfile {
			streamErr := streamPackfile(ctx, st, packReader, req.Progress)
			closeReader(packReader)
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
		if err := updateShallow(st, shallowInfo); err != nil {
			return err
		}
	}

	return nil
}

// streamPackfile demultiplexes the sideband-64k packfile stream into st.
func streamPackfile(ctx context.Context, st storage.Storer, packReader io.Reader, progress sideband.Progress) error {
	reader := ioutil.NewContextReader(ctx, packReader)
	demuxer := sideband.NewDemuxer(sideband.Sideband64k, reader)
	if progress != nil {
		demuxer.Progress = progress
	}
	return packfile.UpdateObjectStorage(st, demuxer)
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

// updateShallow merges a shallow-info update into st's shallow boundary.
func updateShallow(st storage.Storer, info *packp.ShallowUpdate) error {
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
