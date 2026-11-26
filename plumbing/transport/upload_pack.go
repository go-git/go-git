package transport

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// UploadPackRequest is a set of options for the UploadPack service.
type UploadPackRequest struct {
	GitProtocol   string
	AdvertiseRefs bool
	StatelessRPC  bool

	// SkipDeltaCompression disables delta compression when encoding the
	// packfile. When false, the repository pack.window configuration is used.
	//
	// Disabling delta compression significantly improves performance for local
	// transfers where recomputing deltas is unnecessary.
	SkipDeltaCompression bool
}

// UploadPack is a server command that serves the upload-pack service.
func UploadPack(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	opts *UploadPackRequest,
) error {
	if w == nil {
		return fmt.Errorf("nil writer")
	}

	w = ioutil.NewContextWriteCloser(ctx, w)

	if opts == nil {
		opts = &UploadPackRequest{}
	}

	if opts.AdvertiseRefs || !opts.StatelessRPC {
		v := ProtocolVersion(opts.GitProtocol)
		switch v {
		case protocol.V0, protocol.V1, protocol.V2:
			// V0/V1 share the classic advertisement; V2 advertises
			// capabilities only (refs come via ls-refs).
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedVersion, v)
		}

		if v == protocol.V2 {
			if err := AdvertiseCapabilities(ctx, st, w, UploadPackService); err != nil {
				return fmt.Errorf("advertising v2 capabilities: %w", err)
			}
		} else if err := AdvertiseRefs(ctx, st, w, UploadPackService, opts.StatelessRPC, v); err != nil {
			return fmt.Errorf("advertising references: %w", err)
		}
	}

	if opts.AdvertiseRefs {
		// Done, there's nothing else to do
		return nil
	}

	if r == nil {
		return fmt.Errorf("nil reader")
	}

	r = ioutil.NewContextReadCloser(ctx, r)

	rd := bufio.NewReader(r)

	v := ProtocolVersion(opts.GitProtocol)
	if v == protocol.V2 {
		return serveUploadPackV2(ctx, st, rd, w, opts)
	}

	l, _, err := pktline.PeekLine(rd)
	if err != nil {
		return fmt.Errorf("peeking line: %w", err)
	}

	// In case the client has nothing to send, it sends a flush packet to
	// indicate that it is done sending data. In that case, we're done
	// here.
	if l == pktline.Flush {
		return nil
	}

	var done bool
	var haves []plumbing.Hash
	var upreq *packp.UploadRequest
	var common *commonHaves
	var mode ackMode
	var caps capability.List
	var wants []plumbing.Hash
	var plan *shallowPlan
	firstRound := true
	for !done {
		if firstRound {
			upreq = &packp.UploadRequest{}
			if err := upreq.Decode(rd); err != nil {
				return fmt.Errorf("decoding upload-request: %w", err)
			}

			// filter is never advertised, so refuse a filter line as
			// upload-pack does with uploadpack.allowFilter unset.
			if upreq.Filter != "" {
				return fmt.Errorf("%w: %q", ErrFilterNotSupported, upreq.Filter)
			}

			wants = upreq.Wants
			caps = upreq.Capabilities

			if err := r.Close(); err != nil {
				return fmt.Errorf("closing reader: %w", err)
			}

			switch {
			case caps.Supports(capability.MultiACKDetailed):
				mode = ackMultiDetailed
			case caps.Supports(capability.MultiACK):
				mode = ackMulti
			}

			// TODO: support deepen-since, and deepen-not
			if !upreq.Depth.DeepenSince.IsZero() || len(upreq.Depth.DeepenNot) > 0 {
				return fmt.Errorf("unsupported depth: %+v", upreq.Depth)
			}
			plan, err = planShallow(ctx, st, wants, shallowRequest{
				clientShallows: upreq.Shallows,
				depth:          upreq.Depth.Deepen,
			})
			if err != nil {
				return fmt.Errorf("planning shallow fetch: %w", err)
			}

			// A commit that stops being shallow has its history sent down
			// to the new boundary, so its parents are negotiated like wants:
			// they too must reach a common commit before the server says it
			// knows enough. The client's shallow commits and the new
			// boundary end every walk, as nothing beyond them is had or
			// sent.
			negotiated := wants
			var grafts []plumbing.Hash
			if plan != nil {
				negotiated = slices.Concat(wants, plan.extraWants)
				grafts = plan.grafts
			}
			common, err = newCommonHaves(st, negotiated, grafts)
			if err != nil {
				return err
			}

			// A deepen request is always answered with a shallow update,
			// which is just its flush when there is nothing to announce.
			if plan != nil && plan.deepened {
				shupd := packp.ShallowUpdate{Shallows: plan.shallows, Unshallows: plan.unshallows}
				if err := shupd.Encode(w); err != nil {
					return fmt.Errorf("sending shallow-update: %w", err)
				}
			}

			// A stateless client may end its first request here, having
			// asked only for the shallow list, and sends its haves in the
			// next. It is owed neither acknowledgments nor a pack, so the
			// end of input here ends the request without an error.
			if opts.StatelessRPC {
				if _, _, err := pktline.PeekLine(rd); errors.Is(err, io.EOF) {
					return w.Close()
				}
			}
		}

		// UploadHaves.Decode takes EOF for a flush. A stateful client that
		// hangs up mid-negotiation has not finished its request, so report
		// the truncated stream instead of looping on empty rounds. Git's
		// server ends with an error here too.
		if !opts.StatelessRPC {
			if _, _, err := pktline.PeekLine(rd); errors.Is(err, io.EOF) {
				return fmt.Errorf("decoding upload-haves: %w", io.ErrUnexpectedEOF)
			}
		}

		var uphav packp.UploadHaves
		if err := uphav.Decode(rd); err != nil {
			return fmt.Errorf("decoding upload-haves: %w", err)
		}

		if err := r.Close(); err != nil {
			return fmt.Errorf("closing reader: %w", err)
		}

		haves = append(haves, uphav.Haves...)
		done = uphav.Done

		resps, err := acknowledgeHaves(ctx, common, mode, uphav.Haves, done)
		if err != nil {
			return err
		}

		for _, resp := range resps {
			if err := resp.Encode(w); err != nil {
				return fmt.Errorf("sending server-response: %w", err)
			}
		}

		// A stateless round that is not done ends here; the client sends the
		// next round as a new request.
		if opts.StatelessRPC && !done {
			return w.Close()
		}

		firstRound = false
	}

	// Done with the request, now close the reader
	// to indicate that we are done reading from it.
	if err := r.Close(); err != nil {
		return fmt.Errorf("closing reader: %w", err)
	}

	objs, err := plan.objects(st, wants, haves)
	if err != nil {
		_ = w.Close()
		return fmt.Errorf("getting objects to upload: %w", err)
	}

	var (
		useSideband bool
		writer      io.Writer = w
	)
	if caps.Supports(capability.Sideband64k) {
		writer = sideband.NewMuxer(sideband.Sideband64k, w)
		useSideband = true
	} else if caps.Supports(capability.Sideband) {
		writer = sideband.NewMuxer(sideband.Sideband, w)
		useSideband = true
	}

	// TODO: Support shallow-file
	// TODO: Support thin-pack
	var packWindow uint
	if opts.SkipDeltaCompression {
		packWindow = 0
	} else if cfg, cerr := st.Config(); cerr == nil && cfg != nil {
		packWindow = cfg.Pack.Window
	} else {
		packWindow = config.DefaultPackWindow
	}

	e := packfile.NewEncoder(writer, st, false)
	_, err = e.Encode(objs, packWindow)
	if err != nil {
		return fmt.Errorf("encoding packfile: %w", err)
	}

	if useSideband {
		if err := pktline.WriteFlush(w); err != nil {
			return fmt.Errorf("flushing sideband: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("closing writer: %w", err)
	}

	return nil
}

// ackMode is the way a client asked to have its haves acknowledged, by the
// capability it sent with its first want.
type ackMode int

const (
	// ackSingle is the original protocol: the server acknowledges the
	// first have it shares with the client and nothing after it.
	ackSingle ackMode = iota
	// ackMulti is multi_ack: every shared have is acknowledged with
	// "continue".
	ackMulti
	// ackMultiDetailed is multi_ack_detailed: a shared have is
	// acknowledged with "common", and "ready" tells the client that the
	// server knows enough to build a pack.
	ackMultiDetailed
)

// acknowledgeHaves answers one round of a v0 negotiation. It records haves,
// the haves the client sent in this round, in common, and returns the lines
// to send back in order. done says whether the round ended with "done"
// rather than a flush.
//
// A have the server shares is acknowledged at once, in the form mode asks
// for. A have the server lacks is only answered once the server could stop
// negotiating, to tell a multi_ack client that it need not walk further
// down that line of history.
func acknowledgeHaves(
	ctx context.Context,
	common *commonHaves,
	mode ackMode,
	haves []plumbing.Hash,
	done bool,
) ([]packp.ServerResponse, error) {
	var resps []packp.ServerResponse
	ack := func(h plumbing.Hash, status packp.ACKStatus) {
		resps = append(resps, packp.ServerResponse{ACKs: []packp.ACK{{Hash: h, Status: status}}})
	}
	nak := func() { resps = append(resps, packp.ServerResponse{}) }
	knowsEnough := func() (bool, error) {
		enough, err := common.okToGiveUp(ctx)
		if err != nil {
			return false, fmt.Errorf("checking negotiation: %w", err)
		}
		return enough, nil
	}

	sharedInRound, lackedInRound := false, false
	for _, h := range haves {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		shared, err := common.add(h)
		if err != nil {
			return nil, fmt.Errorf("checking have %s: %w", h, err)
		}

		if shared {
			sharedInRound = true
			switch {
			case mode == ackMultiDetailed:
				ack(h, packp.ACKCommon)
			case mode == ackMulti:
				ack(h, packp.ACKContinue)
			case len(common.counted) == 1:
				ack(h, 0)
			}
			continue
		}

		lackedInRound = true
		if mode == ackSingle {
			continue
		}
		enough, err := knowsEnough()
		if err != nil {
			return nil, err
		}
		if enough {
			status := packp.ACKContinue
			if mode == ackMultiDetailed {
				status = packp.ACKReady
			}
			ack(h, status)
		}
	}

	nothingShared := len(common.counted) == 0
	if done {
		// The reply to "done" is the last line before the pack: a NAK when
		// nothing is shared, and for a multi_ack client otherwise the
		// latest shared have once more, without a status.
		switch {
		case nothingShared:
			nak()
		case mode != ackSingle:
			ack(common.last, 0)
		}
		return resps, nil
	}

	// A round in which every have was shared has not said "ready" yet,
	// since that is only attached to a have the server lacks.
	if mode == ackMultiDetailed && sharedInRound && !lackedInRound {
		enough, err := knowsEnough()
		if err != nil {
			return nil, err
		}
		if enough {
			ack(common.last, packp.ACKReady)
		}
	}
	// A flush is answered with a NAK, except that the original protocol
	// stays silent once it has sent its one acknowledgement.
	if nothingShared || mode != ackSingle {
		nak()
	}
	return resps, nil
}

func objectsToUpload(st storage.Storer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	return revlist.Objects(st, wants, haves)
}

// boundaryAtDepth returns the shallow boundary of a fetch limited to depth
// commits: the commits whose shortest distance from heads is exactly depth (a
// head is at depth 1), following every parent. These are the commits the
// client receives without their parents. Heads that do not peel to a commit
// are skipped, and an unlimited depth (unlimitedDepth or more) has no
// boundary.
func boundaryAtDepth(ctx context.Context, st storage.Storer, heads []plumbing.Hash, depth int) ([]plumbing.Hash, error) {
	if depth >= unlimitedDepth {
		return nil, nil
	}

	// Walking breadth-first visits every commit first at its shortest depth,
	// so a commit reached again through a longer path is skipped.
	visited := make(map[plumbing.Hash]struct{})
	var level []plumbing.Hash
	for _, h := range heads {
		c, ok := peelToCommit(st, h)
		if !ok {
			continue
		}
		if _, ok := visited[c.Hash]; ok {
			continue
		}
		visited[c.Hash] = struct{}{}
		level = append(level, c.Hash)
	}

	for d := 1; len(level) > 0; d++ {
		if d == depth {
			plumbing.HashesSort(level)
			return level, nil
		}

		var next []plumbing.Hash
		for _, h := range level {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			c, err := object.GetCommit(st, h)
			if err != nil {
				return nil, fmt.Errorf("getting commit %s: %w", h, err)
			}
			for _, p := range c.ParentHashes {
				if _, ok := visited[p]; ok {
					continue
				}
				visited[p] = struct{}{}
				next = append(next, p)
			}
		}
		level = next
	}

	return nil, nil
}

// clientBoundaryDepth returns the depth, counted from the wants (a tip is at
// depth 1), of the closest commit in the client's shallow set, or 0 if none is
// reachable. A deepen-relative request adds its depth to this value, so that
// the new depth is measured from the client's existing shallow boundary
// rather than from the tips.
func clientBoundaryDepth(ctx context.Context, st storage.Storer, heads, shallows []plumbing.Hash) (int, error) {
	if len(shallows) == 0 {
		return 0, nil
	}
	shallowSet := make(map[plumbing.Hash]struct{}, len(shallows))
	for _, h := range shallows {
		shallowSet[h] = struct{}{}
	}

	// Walking breadth-first reaches every commit first at its shortest
	// depth, so the first level that holds a client shallow commit is the
	// answer and nothing beyond it is read.
	visited := make(map[plumbing.Hash]struct{})
	var level []plumbing.Hash
	for _, h := range heads {
		c, ok := peelToCommit(st, h)
		if !ok {
			continue
		}
		if _, ok := visited[c.Hash]; ok {
			continue
		}
		visited[c.Hash] = struct{}{}
		level = append(level, c.Hash)
	}

	// next is the level being built; the two slices swap after each level.
	var next []plumbing.Hash
	for d := 1; len(level) > 0; d++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		for _, h := range level {
			if _, ok := shallowSet[h]; ok {
				return d, nil
			}
		}

		next = next[:0]
		for _, h := range level {
			// A commit that cannot be read ends its line of history.
			c, err := object.GetCommit(st, h)
			if err != nil {
				continue
			}
			for _, p := range c.ParentHashes {
				if _, ok := visited[p]; ok {
					continue
				}
				visited[p] = struct{}{}
				next = append(next, p)
			}
		}
		level, next = next, level
	}
	return 0, nil
}

// serveUploadPackV2 handles the git protocol v2 for upload-pack (fetch/ls-refs).
// It is used when the client requests version=2 via GIT_PROTOCOL.
func serveUploadPackV2(ctx context.Context, st storage.Storer, rd *bufio.Reader, w io.WriteCloser, opts *UploadPackRequest) error {
	for {
		// Peek the command line to choose the argument decoder, then decode the
		// whole request envelope through packp.CommandRequest (the same type the
		// client encodes).
		l, line, err := pktline.PeekLine(rd)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if l == pktline.Flush {
			// A lone flush-pkt ends the request.
			_, _, _ = pktline.ReadLine(rd)
			return nil
		}

		cmd := strings.TrimPrefix(strings.TrimSuffix(string(line), "\n"), "command=")

		req := &packp.CommandRequest{}
		switch cmd {
		case "ls-refs":
			req.Args = &packp.LsRefsArgs{}
		case "fetch":
			req.Args = &packp.FetchArgs{}
		default:
			_, _ = pktline.Writef(w, "error unknown-command %s\n", cmd)
			_ = pktline.WriteFlush(w)
			return fmt.Errorf("unsupported v2 command %q", cmd)
		}

		if err := req.Decode(rd); err != nil {
			return fmt.Errorf("decoding %s request: %w", cmd, err)
		}

		switch cmd {
		case "ls-refs":
			if err := serveLsRefsV2(ctx, st, w, req.Args.(*packp.LsRefsArgs)); err != nil {
				return err
			}
			// Stateless (HTTP) carries a single command per request; stateful
			// transports may continue, but clients typically close after.
			if opts.StatelessRPC {
				return nil
			}
		case "fetch":
			concluded, err := serveFetchV2(ctx, st, w, req.Args.(*packp.FetchArgs), opts)
			if err != nil {
				return err
			}
			if concluded {
				return nil
			}
			// Stateful transport: the round was acknowledgments-only and the
			// negotiation continues. Loop to read the client's next command.
		}
	}
}

// serveLsRefsV2 responds to a ls-refs command using the decoded arguments.
//
// The reference lines are encoded by writeV2Ref rather than packp.LsRefsOutput:
// a v2 HEAD line carries both a resolved object id and a symref-target
// attribute, which a single plumbing.Reference (hash XOR symbolic) cannot
// represent. writeV2Ref resolves the symref's hash from the storer, so the
// line names the object the symref currently points at.
func serveLsRefsV2(_ context.Context, st storage.Storer, w io.Writer, args *packp.LsRefsArgs) error {
	iter, err := st.IterReferences()
	if err != nil {
		return err
	}
	defer iter.Close()

	var refs []*plumbing.Reference
	if err := iter.ForEach(func(r *plumbing.Reference) error {
		// Use the same name gate as the v0/v1 advertisement. In the v2
		// grammar a space in a name also introduces a ref-attribute.
		if !advertisable(r.Name()) {
			trace.General.Printf("ignoring ref with broken name %q", r.Name().String())
			return nil
		}
		refs = append(refs, r)
		return nil
	}); err != nil {
		return err
	}

	prefixes := args.RefPrefixes

	// HEAD is listed first, but only when it passes the ref-prefix filter:
	// a client that sends prefixes gets HEAD when one of them matches
	// "HEAD", like every other ref. Git's server filters HEAD the same
	// way.
	for _, r := range refs {
		if r.Name() == plumbing.HEAD {
			if len(prefixes) == 0 || refMatchesAnyPrefix(r.Name().String(), prefixes) {
				if err := writeV2Ref(w, st, r, args.Symrefs, args.Peel); err != nil {
					return err
				}
			}
			break
		}
	}

	for _, r := range refs {
		if r.Name() == plumbing.HEAD {
			continue
		}
		if len(prefixes) > 0 && !refMatchesAnyPrefix(r.Name().String(), prefixes) {
			continue
		}
		if err := writeV2Ref(w, st, r, args.Symrefs, args.Peel); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

func refMatchesAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// writeV2Ref writes one line of an ls-refs response for r, with the
// attributes the client asked for. A symbolic reference is listed with the
// object id of the reference it resolves to; one that cannot be resolved, or
// that resolves to no object, is left out.
// See https://git-scm.com/docs/gitprotocol-v2#_ls_refs.
func writeV2Ref(w io.Writer, st storage.Storer, r *plumbing.Reference, symrefs, peel bool) error {
	var hash plumbing.Hash
	var target string
	if r.Type() == plumbing.SymbolicReference {
		ref, err := storer.ResolveReference(st, r.Target())
		if reference.IsUnresolvableForAdvertisement(err) {
			return nil
		}
		if err != nil {
			return err
		}
		hash = ref.Hash()
		target = ref.Name().String()
	} else {
		hash = r.Hash()
	}
	if hash.IsZero() {
		return nil
	}
	// Protocol v2 ls-refs grammar:
	//   ref = obj-id SP refname *(SP ref-attribute) LF
	//   ref-attribute = (symref | peeled)
	// Both symref-target and peeled are attributes on the ref's own line,
	// not separate lines as in the v0/v1 advertisement format. The grammar
	// allows either order; symref-target is written first, as Git's server
	// writes it.
	line := fmt.Sprintf("%s %s", hash, r.Name())
	if symrefs && target != "" && advertisable(plumbing.ReferenceName(target)) {
		line += " symref-target:" + target
	}
	if peel {
		// Peel any ref whose object is an annotated tag, or a chain of
		// them, down to the first object that is not a tag; the ref need
		// not be under refs/tags/. Branches and lightweight tags do not
		// point at tag objects, so they get no attribute. Git's server
		// answers the same way.
		if peeled, ok := peelToNonTag(st, hash); ok {
			line += " peeled:" + peeled.String()
		}
	}
	if _, err := pktline.Writef(w, "%s\n", line); err != nil {
		return err
	}
	return nil
}

// peelToNonTag follows annotated-tag objects from h down to the first non-tag
// object, which is what a "peeled" attribute reports. It returns the peeled
// hash and true when h points at one or more tag objects; false when h is not
// a tag object, as for a branch or a lightweight tag, so that no "peeled"
// attribute is emitted.
func peelToNonTag(st storage.Storer, h plumbing.Hash) (plumbing.Hash, bool) {
	tag, err := object.GetTag(st, h)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	for {
		next := tag.Target
		inner, err := object.GetTag(st, next)
		if err != nil {
			// next is not a tag object, or the server lacks it. Either
			// way the chain of tags ends here and next is the peeled value.
			return next, true
		}
		tag = inner
	}
}

// serveFetchV2 handles command=fetch for v2 using the decoded arguments. The
// acknowledgments, shallow-info, and packfile-header sections are emitted
// through packp.FetchOutput; this function streams the packfile data after the
// header, matching the caller-owned streaming on the client side.
//
// It reports whether the fetch concluded. A packfile (or a terminal no-op)
// returns concluded=true and the connection is closed. An acknowledgments-only
// round on a stateful transport returns concluded=false with the connection
// left open, so the caller loops to read the client's next command=fetch round
// (the stateful negotiation continues until the server is ready). A stateless
// (HTTP) round always concludes, since the client re-POSTs each round.
func serveFetchV2(ctx context.Context, st storage.Storer, w io.WriteCloser, args *packp.FetchArgs, opts *UploadPackRequest) (concluded bool, err error) {
	wants := args.Wants
	haves := args.Haves
	done := args.Done

	// The filter feature of fetch is not advertised, so a client may not
	// send a filter argument. Refuse it before acting on any other
	// argument, rather than send a complete pack that the client takes for
	// a filtered one. Git's server refuses it the same way.
	if args.Filter != "" {
		_ = w.Close()
		return true, fmt.Errorf("%w: %q", ErrFilterNotSupported, args.Filter)
	}

	// No want lines: the client found it needs nothing. There is nothing
	// to answer, so close the stream without writing anything, not even a
	// flush packet. Git's server sends no response here either.
	if len(wants) == 0 {
		return true, w.Close()
	}

	out := &packp.FetchOutput{}

	// Negotiation (acknowledgments section), per gitprotocol-v2 "fetch":
	//
	//   - done            -> no acknowledgments section; packfile follows.
	//   - no haves        -> clone-like; no acknowledgments section; packfile follows.
	//   - haves and !done -> emit an acknowledgments section. ACK every common
	//                        have not already implied by an earlier one.
	//                        "ready" is sent only once every want reaches a
	//                        common have or a parent of one (see
	//                        okToGiveUp); then the packfile follows in the
	//                        same response. Otherwise the section ends without a
	//                        packfile and the client negotiates again with more
	//                        haves (NAK when there is no common object at all).
	if !done && len(haves) > 0 {
		// The acknowledgments of a v2 fetch are decided before its shallow
		// request is worked out, where v0/v1 send the shallow update first.
		// So the parents of unshallowed commits are not negotiated as wants
		// here, and the client's shallow commits do not end the walks. The
		// server's own shallow commits do, and newCommonHaves reads them.
		// Git's server negotiates a v2 fetch in the same order.
		common, err := newCommonHaves(st, wants, nil)
		if err != nil {
			_ = w.Close()
			return true, err
		}
		for _, h := range haves {
			if err := ctx.Err(); err != nil {
				_ = w.Close()
				return true, err
			}
			if _, err := common.add(h); err != nil {
				_ = w.Close()
				return true, fmt.Errorf("checking have %s: %w", h, err)
			}
		}
		out.Acknowledgments = &packp.Acknowledgments{ACKs: common.counted}

		// "ready" is withheld until every want reaches a common have (see
		// okToGiveUp). Declaring it on the first common have would force
		// single-round negotiation and a larger pack. When not ready
		// (including no common object at all, which encodes as NAK), the
		// acknowledgments section stands alone and the client refines its
		// haves in the next request.
		ready, err := common.okToGiveUp(ctx)
		if err != nil {
			_ = w.Close()
			return true, fmt.Errorf("checking negotiation: %w", err)
		}
		if !ready {
			if err := out.Encode(w); err != nil {
				return true, err
			}
			// Stateless (HTTP) carries one round per request: this response is
			// complete and the client re-POSTs the next round. A stateful
			// transport keeps the connection open so the client can send its
			// next command=fetch with refined haves.
			if opts.StatelessRPC {
				return true, w.Close()
			}
			return false, nil
		}
		out.Acknowledgments.Ready = true
	}

	notTips, err := resolveDeepenNot(st, args.DeepenNot)
	if err != nil {
		_ = w.Close()
		return true, fmt.Errorf("resolving deepen-not: %w", err)
	}
	plan, err := planShallow(ctx, st, wants, shallowRequest{
		clientShallows: args.Shallows,
		depth:          args.Deepen,
		relative:       args.DeepenRelative,
		since:          args.DeepenSince,
		notTips:        notTips,
	})
	if err != nil {
		_ = w.Close()
		return true, fmt.Errorf("planning shallow fetch: %w", err)
	}
	// The shallow-info section is written, even when empty, whenever the
	// client asks to deepen or sends a shallow line, as Git's server does.
	// Serving from a shallow repository is not handled.
	if plan != nil {
		out.ShallowInfo = &packp.ShallowInfo{Shallows: plan.shallows, Unshallows: plan.unshallows}
	}

	objs, err := plan.objects(st, wants, haves)
	if err != nil {
		_ = w.Close()
		return true, fmt.Errorf("getting objects to upload: %w", err)
	}

	// include-tag: also pack the annotated tags that point at an object in
	// the pack, so that the client gets the tags of the history it fetches
	// without asking again.
	if args.IncludeTag {
		objs, err = includeReachableTags(st, objs)
		if err != nil {
			_ = w.Close()
			return true, fmt.Errorf("collecting include-tag objects: %w", err)
		}
	}

	// Emit the metadata sections and the "packfile" section header. The client
	// switches to sideband demux after seeing the header, matching reference git.
	out.Packfile = true
	if err := out.Encode(w); err != nil {
		return true, err
	}

	// The packfile is muxed on sideband-64k band 1. This server never writes the
	// progress band (band 2), so the client's no-progress request (args.NoProgress)
	// is honored by construction; there is nothing to suppress.
	writer := sideband.NewMuxer(sideband.Sideband64k, w)

	var packWindow uint
	if opts.SkipDeltaCompression {
		packWindow = 0
	} else if cfg, cerr := st.Config(); cerr == nil && cfg != nil {
		packWindow = cfg.Pack.Window
	} else {
		packWindow = config.DefaultPackWindow
	}

	e := packfile.NewEncoder(writer, st, false)
	if _, err := e.Encode(objs, packWindow); err != nil {
		return true, fmt.Errorf("encoding packfile: %w", err)
	}

	// Terminate the sideband stream and the v2 fetch response.
	if err := pktline.WriteFlush(w); err != nil {
		return true, err
	}

	return true, w.Close()
}

// unlimitedDepth is the depth a client sends to ask for complete history, as
// git fetch --unshallow does: the largest value a 32-bit signed depth holds.
// A request for that depth or more has no shallow boundary.
const unlimitedDepth = 0x7fffffff

// shallowRequest is the shallow part of a fetch request; v0/v1 and v2 requests
// both map onto it.
type shallowRequest struct {
	clientShallows []plumbing.Hash
	depth          int
	relative       bool
	since          time.Time
	notTips        []plumbing.Hash
}

// shallowPlan bounds the objects sent to a shallow or deepening client. It is
// computed from the wants alone, before negotiation, because v0/v1 send the
// shallow update ahead of reading the client's haves.
type shallowPlan struct {
	// deepened reports whether the client asked to deepen, and so is owed the
	// shallow and unshallow lines below.
	deepened   bool
	shallows   []plumbing.Hash
	unshallows []plumbing.Hash

	// grafts are walked as parentless commits: the client's shallow commits,
	// beyond which it has nothing, and the new boundary, beyond which nothing
	// is sent.
	grafts []plumbing.Hash
	// extraWants are the parents of unshallowed commits. The graft on an
	// unshallowed commit stops the walk from the client's haves, while its
	// history down to the new boundary is still sent from here.
	extraWants []plumbing.Hash
}

// planShallow computes the shallow boundary, the unshallowed commits and the
// object-walk grafts for a fetch of wants. A request takes one of three
// shapes: shallow lines alone, from a shallow client that does not deepen;
// a boundary by date or exclusion (deepen-since, deepen-not); or a boundary
// at a depth, which deepen-relative counts from the client's own boundary.
//
// It returns a nil plan when the client neither is shallow nor asks to
// deepen, and an error when deepen is combined with deepen-since or
// deepen-not. The walks stop with ctx's error once ctx is done.
func planShallow(ctx context.Context, st storage.Storer, wants []plumbing.Hash, req shallowRequest) (*shallowPlan, error) {
	var err error
	if req.clientShallows, err = knownClientShallows(st, req.clientShallows); err != nil {
		return nil, err
	}
	byDepth := req.depth > 0
	byDateOrExclusion := !req.since.IsZero() || len(req.notTips) > 0
	if byDepth && byDateOrExclusion {
		return nil, packp.ErrDeepenMutuallyExclusive
	}
	if !byDepth && !byDateOrExclusion && len(req.clientShallows) == 0 {
		return nil, nil
	}

	plan := &shallowPlan{grafts: req.clientShallows}
	switch {
	case !byDepth && !byDateOrExclusion:
		// Shallow lines without a deepen: the client's boundary only bounds
		// what it has.
		return plan, nil
	case byDateOrExclusion:
		plan.shallows, err = boundaryByDateOrExclusion(ctx, st, wants, req.since, req.notTips)
	default:
		depth := req.depth
		if depth >= unlimitedDepth && len(req.clientShallows) > 0 {
			shallows, serr := st.Shallow()
			if serr != nil {
				return nil, fmt.Errorf("reading shallow commits: %w", serr)
			}
			if len(shallows) == 0 {
				// A complete repository answers a request for unlimited
				// depth by unshallowing every shallow commit the client
				// named, reachable from the wants or not, as Git's server
				// does.
				plan.deepened = true
				for _, h := range req.clientShallows {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					c, cerr := object.GetCommit(st, h)
					if cerr != nil {
						return nil, fmt.Errorf("getting commit %s: %w", h, cerr)
					}
					plan.unshallows = append(plan.unshallows, h)
					plan.extraWants = append(plan.extraWants, c.ParentHashes...)
				}
				return plan, nil
			}
		}
		if req.relative && len(req.clientShallows) > 0 {
			// deepen-relative counts from the client's boundary: offset the
			// depth by that boundary's distance from the wants.
			cur, ferr := clientBoundaryDepth(ctx, st, wants, req.clientShallows)
			if ferr != nil {
				return nil, fmt.Errorf("computing shallow frontier depth: %w", ferr)
			}
			if cur == 0 {
				// No client shallow commit is reachable from the wants, so
				// there is no boundary to count from and the client's view
				// stays as it is.
				return plan, nil
			}
			depth += cur
		}
		plan.shallows, err = boundaryAtDepth(ctx, st, wants, depth)
	}
	if err != nil {
		return nil, fmt.Errorf("computing shallow commits: %w", err)
	}

	plan.deepened = true
	plan.unshallows, plan.extraWants, err = commitsNoLongerShallow(ctx, st, wants, req.clientShallows, plan.shallows)
	if err != nil {
		return nil, fmt.Errorf("computing unshallowed commits: %w", err)
	}
	plan.grafts = slices.Concat(req.clientShallows, plan.shallows)
	clientShallows := make(map[plumbing.Hash]struct{}, len(req.clientShallows))
	for _, h := range req.clientShallows {
		clientShallows[h] = struct{}{}
	}
	// The client already records its own shallow commits, so only the
	// ones new to it are announced.
	plan.shallows = slices.DeleteFunc(plan.shallows, func(h plumbing.Hash) bool {
		_, ok := clientShallows[h]
		return ok
	})
	return plan, nil
}

// knownClientShallows returns the client's shallow commits that st has, each
// once. A shallow line naming an object the server does not have is ignored,
// since no walk here can reach it; a repeated one is collapsed; and one
// naming an object that is not a commit is an error.
func knownClientShallows(st storage.Storer, shallows []plumbing.Hash) ([]plumbing.Hash, error) {
	if len(shallows) == 0 {
		return shallows, nil
	}
	known := make([]plumbing.Hash, 0, len(shallows))
	seen := make(map[plumbing.Hash]struct{}, len(shallows))
	for _, h := range shallows {
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		_, err := object.GetCommit(st, h)
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			// A typed lookup reports another type as not found too.
			if st.HasEncodedObject(h) == nil {
				return nil, fmt.Errorf("invalid shallow object %s", h)
			}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("getting commit %s: %w", h, err)
		}
		known = append(known, h)
	}
	return known, nil
}

// objects returns the objects to pack for a plan, or for an unbounded fetch
// when p is nil.
func (p *shallowPlan) objects(st storage.Storer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	if p == nil {
		return objectsToUpload(st, wants, haves)
	}
	wants = slices.Concat(wants, p.extraWants)
	return revlist.ObjectsWithShallows(st, wants, haves, p.grafts)
}

// commitsNoLongerShallow returns the client's shallow commits that a deepen
// to boundary makes interior, and their parents. Such a commit is reachable
// from the wants without crossing the boundary, and is not on it; the client
// is told to unshallow it and is sent its parents.
func commitsNoLongerShallow(ctx context.Context, st storage.Storer, wants, clientShallows, boundary []plumbing.Hash) (unshallows, parents []plumbing.Hash, err error) {
	if len(clientShallows) == 0 {
		return nil, nil, nil
	}

	stop := make(map[plumbing.Hash]struct{}, len(boundary))
	for _, h := range boundary {
		stop[h] = struct{}{}
	}
	within := make(map[plumbing.Hash]struct{})
	var stack []plumbing.Hash
	for _, h := range wants {
		if c, ok := peelToCommit(st, h); ok {
			stack = append(stack, c.Hash)
		}
	}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := within[h]; ok {
			continue
		}
		within[h] = struct{}{}
		if _, ok := stop[h]; ok {
			continue
		}
		c, err := object.GetCommit(st, h)
		if err != nil {
			return nil, nil, fmt.Errorf("getting commit %s: %w", h, err)
		}
		stack = append(stack, c.ParentHashes...)
	}

	for _, h := range clientShallows {
		if _, ok := within[h]; !ok {
			continue
		}
		if _, ok := stop[h]; ok {
			continue
		}
		c, err := object.GetCommit(st, h)
		if err != nil {
			return nil, nil, fmt.Errorf("getting commit %s: %w", h, err)
		}
		unshallows = append(unshallows, h)
		parents = append(parents, c.ParentHashes...)
	}
	return unshallows, parents, nil
}

// resolveDeepenNot resolves each deepen-not argument to the object it names,
// peeling annotated tags. The history reachable from these objects is what
// the shallow fetch leaves out.
//
// An argument is a ref name, full or short: it is looked up the way a
// revision name is, as itself, under refs/, as a tag, as a branch and as a
// remote-tracking branch. It has to name exactly one ref; a name that names
// none, or several, is an error, and so is an object id, as on Git's server.
func resolveDeepenNot(st storage.Storer, refs []string) ([]plumbing.Hash, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]plumbing.Hash, 0, len(refs))
	for _, r := range refs {
		var h plumbing.Hash
		found := 0
		for _, rule := range plumbing.RefRevParseRules {
			ref, err := storer.ResolveReference(st, plumbing.ReferenceName(fmt.Sprintf(rule, r)))
			if err != nil {
				continue
			}
			h = ref.Hash()
			found++
		}
		switch {
		case found == 0:
			return nil, fmt.Errorf("cannot resolve deepen-not %q", r)
		case found > 1:
			return nil, fmt.Errorf("deepen-not %q names more than one reference", r)
		}
		if peeled, ok := peelToNonTag(st, h); ok {
			h = peeled
		}
		out = append(out, h)
	}
	return out, nil
}

// reachableCommits returns the set of commits reachable from tips (inclusive),
// used as the exclusion set for deepen-not.
func reachableCommits(ctx context.Context, st storage.Storer, tips []plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	seen := make(map[plumbing.Hash]struct{})
	stack := append([]plumbing.Hash(nil), tips...)
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		c, err := object.GetCommit(st, h)
		if err != nil {
			continue
		}
		stack = append(stack, c.ParentHashes...)
	}
	return seen, nil
}

// boundaryByDateOrExclusion computes the shallow boundary for a deepen-since
// and/or deepen-not request. The included set is every commit reachable from
// heads that is not older than since (when set) and not reachable from any
// notTips (when set); a commit in the set with a parent outside it is a
// shallow boundary.
//
// The walk stops at the first commit older than since on each path. Git's
// server looks a little further, to tolerate committer timestamps that are
// out of order, so under clock skew the boundary may differ by a few
// commits; the resulting shallow clone is still valid.
func boundaryByDateOrExclusion(ctx context.Context, st storage.Storer, heads []plumbing.Hash, since time.Time, notTips []plumbing.Hash) ([]plumbing.Hash, error) {
	exclude, err := reachableCommits(ctx, st, notTips)
	if err != nil {
		return nil, err
	}

	included := make(map[plumbing.Hash]struct{})
	parents := make(map[plumbing.Hash][]plumbing.Hash)
	visited := make(map[plumbing.Hash]struct{})
	stack := append([]plumbing.Hash(nil), heads...)
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := visited[h]; ok {
			continue
		}
		visited[h] = struct{}{}
		if _, ex := exclude[h]; ex {
			continue
		}
		c, err := object.GetCommit(st, h)
		if err != nil {
			continue
		}
		if !since.IsZero() && c.Committer.When.Before(since) {
			continue
		}
		included[h] = struct{}{}
		parents[h] = c.ParentHashes
		stack = append(stack, c.ParentHashes...)
	}

	var shallows []plumbing.Hash
	for h := range included {
		for _, p := range parents[h] {
			if _, ok := included[p]; !ok {
				shallows = append(shallows, h)
				break
			}
		}
	}
	plumbing.HashesSort(shallows)
	return shallows, nil
}

// includeReachableTags implements the fetch "include-tag" feature: for every
// tag ref whose annotated tag leads, directly or through further tags, to an
// object already in objs, it adds the tag objects on the way there. This is
// the rule gitprotocol-capabilities gives for include-tag: a tag is packed
// when the object it points at is. Lightweight tags have no tag object and
// are skipped.
func includeReachableTags(st storage.Storer, objs []plumbing.Hash) ([]plumbing.Hash, error) {
	have := make(map[plumbing.Hash]struct{}, len(objs))
	for _, h := range objs {
		have[h] = struct{}{}
	}

	iter, err := st.IterReferences()
	if err != nil {
		return objs, err
	}
	defer iter.Close()

	added := objs
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference || !ref.Name().IsTag() {
			return nil
		}
		var chain []plumbing.Hash
		seen := make(map[plumbing.Hash]struct{})
		cur := ref.Hash()
		for {
			if _, ok := have[cur]; ok {
				// Reached an object already in the pack: include the tag
				// objects that point at it.
				for _, t := range chain {
					if _, ok := have[t]; !ok {
						have[t] = struct{}{}
						added = append(added, t)
					}
				}
				break
			}
			if _, ok := seen[cur]; ok {
				break // defend against a tag cycle in a malformed repo
			}
			seen[cur] = struct{}{}
			tag, terr := object.GetTag(st, cur)
			if terr != nil {
				break // non-tag object not in the pack: nothing to add
			}
			chain = append(chain, cur)
			cur = tag.Target
		}
		return nil
	})
	if err != nil {
		return objs, err
	}
	return added, nil
}

// commonHaves is the negotiation state of a fetch: the client's haves that
// the server also has, and the last answer of okToGiveUp.
type commonHaves struct {
	st storage.Storer
	// wants are the objects the client asked for.
	wants []plumbing.Hash
	// grafts are the commits taken as parentless.
	grafts map[plumbing.Hash]struct{}

	// counted holds the common haves not already implied by an earlier one.
	counted []plumbing.Hash
	// clientHas holds every common have and the parents of common commits.
	clientHas map[plumbing.Hash]struct{}
	// oldest is the committer date, in seconds since the epoch, of the oldest
	// common commit. Zero means no common commit yet, so a commit dated zero
	// does not pin it.
	oldest int64
	// last is the most recent common have.
	last plumbing.Hash
	// added holds every have add has processed and whether the server has
	// it, so a repeated have is not read again.
	added map[plumbing.Hash]bool

	// nodes holds the commits read so far, nil for a commit the server does
	// not have.
	nodes map[plumbing.Hash]*commitNode
	// checked reports that ready holds the answer of okToGiveUp for the
	// current clientHas and oldest.
	checked bool
	ready   bool

	// tips holds the commits the wants peel to, oldest first, once peeled
	// is set.
	tips   []*commitNode
	peeled bool
	// walk numbers the checks okToGiveUp has run. A commitNode's marks
	// count only when they carry the number of the current one, so a new
	// check starts without marks and without clearing any.
	walk uint
	// path is the stack of the last walk, kept for its capacity.
	path []walkStep
}

// newCommonHaves returns the negotiation state of a fetch of wants, before
// any have is known, in which the commits in grafts and the server's own
// shallow commits have no parents. A shallow repository holds no history
// beyond its shallow commits, whatever the request says, so they bound the
// walks in every protocol version. Git's server treats its own shallow
// commits the same way.
func newCommonHaves(st storage.Storer, wants, grafts []plumbing.Hash) (*commonHaves, error) {
	shallows, err := st.Shallow()
	if err != nil {
		return nil, fmt.Errorf("reading shallow commits: %w", err)
	}
	c := &commonHaves{
		st:        st,
		wants:     wants,
		grafts:    make(map[plumbing.Hash]struct{}, len(grafts)+len(shallows)),
		clientHas: map[plumbing.Hash]struct{}{},
		added:     map[plumbing.Hash]bool{},
		nodes:     map[plumbing.Hash]*commitNode{},
	}
	for _, h := range slices.Concat(grafts, shallows) {
		c.grafts[h] = struct{}{}
	}
	return c, nil
}

// commitNode is what the negotiation needs of a commit.
type commitNode struct {
	// date is the committer date, in seconds since the epoch.
	date int64
	// parents are none for a grafted commit.
	parents []plumbing.Hash
	// clientHas reports whether the commit is in commonHaves.clientHas.
	clientHas bool
	// parentNodes holds the nodes of the first resolved parents, nil for a
	// parent the server lacks. A walk looks at the parents in order, so a
	// later check follows the pointers instead of looking each parent up.
	parentNodes []*commitNode
	resolved    int

	// entered is the check in which a walk entered the commit, or passed
	// it over as older than the cutoff. It is not entered again in that
	// check.
	entered uint
	// reaches is the check in which a walk found the commit to descend
	// from one the client has.
	reaches uint
}

// remember records commit in nodes and returns its node.
func (c *commonHaves) remember(commit *object.Commit) *commitNode {
	n := &commitNode{date: commit.Committer.When.Unix(), parents: commit.ParentHashes}
	if _, ok := c.grafts[commit.Hash]; ok {
		n.parents = nil
	}
	_, n.clientHas = c.clientHas[commit.Hash]
	c.nodes[commit.Hash] = n
	return n
}

// markClientHas adds h to clientHas and reports whether it was not there yet.
func (c *commonHaves) markClientHas(h plumbing.Hash) bool {
	if _, ok := c.clientHas[h]; ok {
		return false
	}
	c.clientHas[h] = struct{}{}
	if n := c.nodes[h]; n != nil {
		n.clientHas = true
	}
	return true
}

// node returns the node of commit h, reading it only if nodes does not hold
// it yet, or nil if the server does not have h.
func (c *commonHaves) node(h plumbing.Hash) (*commitNode, error) {
	if n, ok := c.nodes[h]; ok {
		return n, nil
	}
	commit, err := object.GetCommit(c.st, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		c.nodes[h] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c.remember(commit), nil
}

// add records the have h and reports whether the server has it, returning an
// error if the object cannot be read. The parents of a common commit count
// as had by the client before the commit itself is checked. A have that adds
// nothing to clientHas and does not move oldest, such as a repeated have or a
// blob, leaves checked set, so the last answer of okToGiveUp stands.
//
// A have add has already processed is answered without reading it again. Of
// an object that is not a known commit only the type is looked up, so a have
// naming a large blob is not inflated; a commit is then read.
func (c *commonHaves) add(h plumbing.Hash) (bool, error) {
	if had, ok := c.added[h]; ok {
		if had {
			c.last = h
		}
		return had, nil
	}

	n, isCommit := c.nodes[h]
	if !isCommit || n == nil {
		typ, err := storer.EncodedObjectType(c.st, h)
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			c.added[h] = false
			return false, nil
		}
		if err != nil {
			return false, err
		}
		isCommit = typ == plumbing.CommitObject
		if isCommit {
			commit, err := object.GetCommit(c.st, h)
			if err != nil {
				return false, err
			}
			n = c.remember(commit)
		}
	}
	c.added[h] = true

	c.last = h
	if isCommit {
		if c.oldest == 0 || n.date < c.oldest {
			c.oldest = n.date
			c.checked = false
		}
		for _, p := range n.parents {
			if c.markClientHas(p) {
				c.checked = false
			}
		}
	}
	if c.markClientHas(h) {
		c.counted = append(c.counted, h)
		if isCommit {
			c.checked = false
		}
	}
	return true, nil
}

// okToGiveUp reports whether the client has told the server enough for
// negotiation to stop: every want that peels to a commit either is had by the
// client or descends from a commit the client has. From then on more haves
// could only shrink the pack, not connect it, so the server answers "ready"
// (or "continue" under multi_ack) and the client may send "done". It is false
// while no have is common.
//
// The answer is searched for the way git's server does, because the round in
// which a server says "ready" is visible to the client. The search is not a
// plain ancestry test: it is bounded and it shares its marks between wants,
// so on histories whose commit dates disagree with their order it can miss a
// path that exists. Those misses are part of the answer and are kept:
//
//   - The wants are walked one after the other, oldest committer date first,
//     and the check fails at the first want that reaches nothing the client
//     has.
//   - A walk goes depth-first through the parents, in the order the commit
//     lists them. It does not enter a parent dated before the oldest common
//     commit, since a commit older than everything the client is known to
//     have is not expected to descend from any of it. A parent dated the
//     same second is entered. The bound applies to parents only, so a want
//     is walked whatever its date.
//   - A commit is entered at most once per check, whichever want the walk
//     started from. Meeting it again only tells whether it was found to
//     reach a common commit, which it may not have been when it was entered
//     with a higher cutoff ahead of it or was skipped as too old.
//   - A commit learns that a parent reaches a common commit when it looks at
//     that parent, not when the walk returns from it. So once the walk is
//     back at a commit, it still enters that commit's next unvisited parent
//     before leaving it.
//
// The marks live for one check only. The answer is kept until add reports a
// change that can alter it, and commits are read through nodes, so a later
// check costs no object reads for commits an earlier one saw. Each check
// enters a commit at most once and looks at each parent link at most twice.
//
// The answers are those git gives for a repository without a commit-graph
// file. With one, git can say "ready" in a different round on a history whose
// dates run against its parent links. The commit-graph is not consulted here,
// so the answer does not depend on the file. Only the round in which "ready"
// is sent differs, not what is fetched.
func (c *commonHaves) okToGiveUp(ctx context.Context) (bool, error) {
	if len(c.clientHas) == 0 {
		return false, nil
	}
	if c.checked {
		return c.ready, nil
	}
	if err := c.peelWants(); err != nil {
		return false, err
	}

	c.walk++
	ready := true
	for _, tip := range c.tips {
		reaches, err := c.reachesCommon(ctx, tip)
		if err != nil {
			return false, err
		}
		if !reaches {
			ready = false
			break
		}
	}
	c.checked, c.ready = true, ready
	return ready, nil
}

// peelWants fills tips, once, with the commits the wants peel to, oldest
// first and otherwise in request order. A want that peels to no commit asks
// for no history, so it cannot hold readiness back.
func (c *commonHaves) peelWants() error {
	if c.peeled {
		return nil
	}
	tips := make([]*commitNode, 0, len(c.wants))
	for _, h := range c.wants {
		commit, err := peelWant(c.st, h)
		if err != nil {
			return fmt.Errorf("peeling want %s: %w", h, err)
		}
		if commit == nil {
			continue
		}
		n := c.nodes[commit.Hash]
		if n == nil {
			n = c.remember(commit)
		}
		tips = append(tips, n)
	}
	slices.SortStableFunc(tips, func(a, b *commitNode) int {
		return cmp.Compare(a.date, b.date)
	})
	c.tips, c.peeled = tips, true
	return nil
}

// walkStep is a commit a walk is in, and the parent it looks at next.
type walkStep struct {
	node *commitNode
	next int
}

// reachesCommon walks from tip as okToGiveUp describes and reports whether
// tip is had by the client or was found to descend from a commit it has. It
// honours the marks earlier walks of the same check left, and leaves its own.
func (c *commonHaves) reachesCommon(ctx context.Context, tip *commitNode) (bool, error) {
	tip.entered = c.walk
	path := c.path[:0]
	path = append(path, walkStep{node: tip})
	defer func() { c.path = path[:0] }()

	for len(path) > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		step := &path[len(path)-1]
		at := step.node
		if at.clientHas || at.reaches == c.walk {
			// Nothing below a commit known to reach the client's history
			// needs walking, and its child on the path reaches it too.
			path = path[:len(path)-1]
			if len(path) > 0 {
				path[len(path)-1].node.reaches = c.walk
			}
			continue
		}

		var enter *commitNode
		for ; step.next < len(at.parents); step.next++ {
			h := at.parents[step.next]
			var parent *commitNode
			if step.next < at.resolved {
				parent = at.parentNodes[step.next]
			} else {
				var err error
				if parent, err = c.node(h); err != nil {
					return false, fmt.Errorf("getting commit %s: %w", h, err)
				}
				if at.parentNodes == nil {
					at.parentNodes = make([]*commitNode, len(at.parents))
				}
				at.parentNodes[step.next] = parent
				at.resolved = step.next + 1
			}
			if parent == nil {
				// The server lacks this parent, but the client may have
				// named a child of it, and then has it.
				if _, ok := c.clientHas[h]; ok {
					at.reaches = c.walk
				}
				continue
			}
			if parent.clientHas || parent.reaches == c.walk {
				at.reaches = c.walk
			}
			if parent.entered == c.walk {
				continue
			}
			parent.entered = c.walk
			// Zero means no common commit yet, and then nothing is too old.
			if c.oldest != 0 && parent.date < c.oldest {
				continue
			}
			enter = parent
			break
		}
		if enter == nil {
			path = path[:len(path)-1]
			continue
		}
		// next stays on the entered parent: when the walk is back here, that
		// parent is looked at again for what the walk below it found.
		path = append(path, walkStep{node: enter})
	}
	return tip.clientHas || tip.reaches == c.walk, nil
}

// peelWant resolves the want h to a commit, following annotated tags. It
// returns no commit, and no error, when h peels to an object that is not a
// commit, or to a tag whose target the server does not have. Any other
// failure to read h, a tag or the commit is returned.
func peelWant(st storage.Storer, h plumbing.Hash) (*object.Commit, error) {
	for want := true; ; want = false {
		typ, err := storer.EncodedObjectType(st, h)
		if !want && errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		switch typ {
		case plumbing.CommitObject:
			return object.GetCommit(st, h)
		case plumbing.TagObject:
			tag, err := object.GetTag(st, h)
			if err != nil {
				return nil, err
			}
			h = tag.Target
		default:
			return nil, nil
		}
	}
}

// peelToCommit resolves h to a commit, following annotated tags. It returns
// false when h is missing or does not peel to a commit.
func peelToCommit(st storage.Storer, h plumbing.Hash) (*object.Commit, bool) {
	obj, err := st.EncodedObject(plumbing.AnyObject, h)
	if err != nil {
		return nil, false
	}
	switch obj.Type() {
	case plumbing.CommitObject:
		c, err := object.GetCommit(st, h)
		if err != nil {
			return nil, false
		}
		return c, true
	case plumbing.TagObject:
		tag, err := object.GetTag(st, h)
		if err != nil {
			return nil, false
		}
		return peelToCommit(st, tag.Target)
	default:
		return nil, false
	}
}
