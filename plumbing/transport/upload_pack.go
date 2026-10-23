package transport

import (
	"bufio"
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
	var anyMultiAck, multiAckDetailed bool
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

			wants = upreq.Wants
			caps = upreq.Capabilities

			if err := r.Close(); err != nil {
				return fmt.Errorf("closing reader: %w", err)
			}

			multiAckDetailed = caps.Supports(capability.MultiACKDetailed)
			// Either form, where upstream tests data->multi_ack.
			anyMultiAck = multiAckDetailed || caps.Supports(capability.MultiACK)

			// TODO: support deepen-since, and deepen-not
			if !upreq.Depth.DeepenSince.IsZero() || len(upreq.Depth.DeepenNot) > 0 {
				return fmt.Errorf("unsupported depth: %+v", upreq.Depth)
			}
			plan, err = planShallow(st, wants, shallowRequest{
				clientShallows: upreq.Shallows,
				depth:          upreq.Depth.Deepen,
			})
			if err != nil {
				return fmt.Errorf("planning shallow fetch: %w", err)
			}

			// Before negotiation starts, upstream send_unshallow adds the
			// parents of unshallowed commits to want_obj, so ok_to_give_up
			// requires them to reach a common commit too, and the client's
			// shallow commits and the new boundary are registered as
			// shallow, so neither walk goes past them.
			negotiated := wants
			var grafts []plumbing.Hash
			if plan != nil {
				negotiated = slices.Concat(wants, plan.extraWants)
				grafts = plan.grafts
			}
			common = newCommonHaves(st, negotiated, grafts)

			// Upstream follows a deepen with the shallow update and a flush
			// even when both lists are empty (receive_needs).
			if plan != nil && plan.deepened {
				shupd := packp.ShallowUpdate{Shallows: plan.shallows, Unshallows: plan.unshallows}
				if err := shupd.Encode(w); err != nil {
					return fmt.Errorf("sending shallow-update: %w", err)
				}
			}
		}

		// UploadHaves.Decode takes EOF for a flush. Upstream dies when a
		// stateful client hangs up mid-negotiation, so do the same instead of
		// looping on empty rounds.
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

		// Acknowledge the haves as upstream get_common_commits does: a have is
		// common when the server has it, and "ready" is only promised once
		// every want reaches a common have (ok_to_give_up).
		var resps []packp.ServerResponse
		ack := func(a packp.ACK) {
			resps = append(resps, packp.ServerResponse{ACKs: []packp.ACK{a}})
		}
		nak := func() { resps = append(resps, packp.ServerResponse{}) }
		gotCommon, gotOther := false, false
		for _, h := range uphav.Haves {
			ok, err := common.add(h)
			if err != nil {
				return fmt.Errorf("checking have %s: %w", h, err)
			}
			if !ok {
				gotOther = true
				if anyMultiAck {
					ready, err := common.okToGiveUp(ctx)
					if err != nil {
						return fmt.Errorf("checking negotiation: %w", err)
					}
					if ready {
						status := packp.ACKContinue
						if multiAckDetailed {
							status = packp.ACKReady
						}
						ack(packp.ACK{Hash: h, Status: status})
					}
				}
				continue
			}

			gotCommon = true
			switch {
			case multiAckDetailed:
				ack(packp.ACK{Hash: h, Status: packp.ACKCommon})
			case anyMultiAck:
				ack(packp.ACK{Hash: h, Status: packp.ACKContinue})
			case len(common.counted) == 1:
				ack(packp.ACK{Hash: h})
			}
		}

		if done {
			switch {
			case len(common.counted) == 0:
				nak()
			case anyMultiAck:
				ack(packp.ACK{Hash: common.last})
			}
		} else {
			if multiAckDetailed && gotCommon && !gotOther {
				ready, err := common.okToGiveUp(ctx)
				if err != nil {
					return fmt.Errorf("checking negotiation: %w", err)
				}
				if ready {
					ack(packp.ACK{Hash: common.last, Status: packp.ACKReady})
				}
			}
			if len(common.counted) == 0 || anyMultiAck {
				nak()
			}
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

func objectsToUpload(st storage.Storer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	return revlist.Objects(st, wants, haves)
}

// getShallowCommits returns the shallow boundary of a fetch limited to depth
// commits: the commits whose shortest distance from heads is exactly depth (a
// head is at depth 1), following every parent. It mirrors upstream
// get_shallows_or_depth (shallow.c), the walk behind get_shallow_commits.
// Heads that do not peel to a commit are skipped, and an infinite depth
// (infiniteDepth or more) has no boundary.
func getShallowCommits(st storage.Storer, heads []plumbing.Hash, depth int) ([]plumbing.Hash, error) {
	if depth >= infiniteDepth {
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

// shallowFrontierDepth returns the depth, counted from the wants (a tip is at
// depth 1), of the closest commit in the client's shallow set, or 0 if none is
// reachable. It mirrors upstream get_shallows_depth (shallow.c): the value
// offsets a deepen-relative request so the new depth is measured from the
// client's existing shallow boundary rather than from the tips.
func shallowFrontierDepth(st storage.Storer, heads, shallows []plumbing.Hash) (int, error) {
	shallowSet := make(map[plumbing.Hash]struct{}, len(shallows))
	for _, h := range shallows {
		shallowSet[h] = struct{}{}
	}

	best := 0
	seen := map[plumbing.Hash]int{}
	type frame struct {
		hash  plumbing.Hash
		depth int // depth of this commit's predecessor; the commit sits at depth+1
	}
	var stack []frame
	for _, h := range heads {
		if c, ok := peelToCommit(st, h); ok {
			stack = append(stack, frame{c.Hash, 0})
		}
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if d, ok := seen[f.hash]; ok && d <= f.depth {
			continue
		}
		seen[f.hash] = f.depth

		cur := f.depth + 1
		if _, ok := shallowSet[f.hash]; ok {
			if best == 0 || cur < best {
				best = cur
			}
			// A client shallow commit is a normal commit on the server, so the
			// walk continues past it, matching upstream get_shallows_or_depth.
		}

		c, err := object.GetCommit(st, f.hash)
		if err != nil {
			continue
		}
		for _, p := range c.ParentHashes {
			stack = append(stack, frame{p, cur})
		}
	}
	return best, nil
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
// represent. writeV2Ref resolves the symref's hash from the storer, matching
// upstream git's send_ref.
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

	// HEAD is emitted first, but only when it passes the ref-prefix filter,
	// matching upstream's send_possibly_unborn_head -> send_ref (ls-refs.c),
	// where HEAD is subject to ref_match like every other ref.
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

// writeV2Ref writes an ls-refs response with the requested reference attributes.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/ls-refs.c#L91-L117.
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
	// Both symref-target and peeled are attributes on the ref's own line
	// (symref-target first, matching upstream's send_ref ordering), not
	// separate lines as in the v0/v1 advertisement format.
	line := fmt.Sprintf("%s %s", hash, r.Name())
	if symrefs && target != "" && advertisable(plumbing.ReferenceName(target)) {
		line += " symref-target:" + target
	}
	if peel {
		// Peel any ref whose object is (a chain of) annotated tags, not just
		// refs/tags/*, and resolve all the way to the underlying non-tag object
		// — matching upstream's reference_get_peeled_oid (ls-refs.c). Lightweight
		// tags and branches don't point at tag objects, so they emit no attribute.
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
// object, mirroring upstream's reference_get_peeled_oid. It returns the peeled
// hash and true when h points at one or more tag objects; false when h is not a
// tag (a lightweight tag, branch, etc.) so no "peeled" attribute is emitted.
func peelToNonTag(st storage.Storer, h plumbing.Hash) (plumbing.Hash, bool) {
	tag, err := object.GetTag(st, h)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	for {
		next := tag.Target
		inner, err := object.GetTag(st, next)
		if err != nil {
			// next is a non-tag object (or missing); return it as the peeled
			// value, as upstream's peel does.
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

	// No 'want' lines: the client guessed it didn't want anything. Upstream
	// emits no response at all here (upload-pack.c, UPLOAD_DONE), so write
	// nothing and just close the stream, no stray flush packet.
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
	//                        common have or a parent of one (upstream's
	//                        ok_to_give_up); then the packfile follows in the
	//                        same response. Otherwise the section ends without a
	//                        packfile and the client negotiates again with more
	//                        haves (NAK when there is no common object at all).
	if !done && len(haves) > 0 {
		// Unlike v0/v1, upstream negotiates before send_shallow_info adds the
		// parents of unshallowed commits to the wants and registers the
		// client's shallow commits, so neither applies here.
		common := newCommonHaves(st, wants, nil)
		for _, h := range haves {
			if _, err := common.add(h); err != nil {
				_ = w.Close()
				return true, fmt.Errorf("checking have %s: %w", h, err)
			}
		}
		out.Acknowledgments = &packp.Acknowledgments{ACKs: common.counted}

		// "ready" is withheld until every want reaches a common have
		// (upstream's ok_to_give_up). Declaring it on the first common
		// have would force single-round negotiation and a larger pack. When not
		// ready (including no common object at all, which encodes as NAK), the
		// acknowledgments section stands alone and the client refines its haves
		// in the next request.
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
	plan, err := planShallow(st, wants, shallowRequest{
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
	// Upstream send_shallow_info writes the section, even when empty, when
	// there is a deepen request or a client shallow line. Serving from a
	// shallow repository is not handled.
	if plan != nil {
		out.ShallowInfo = &packp.ShallowInfo{Shallows: plan.shallows, Unshallows: plan.unshallows}
	}

	objs, err := plan.objects(st, wants, haves)
	if err != nil {
		_ = w.Close()
		return true, fmt.Errorf("getting objects to upload: %w", err)
	}

	// include-tag: add annotated tags whose target is in the pack (auto-tag
	// following), mirroring upstream pack-objects --include-tag.
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

// infiniteDepth is git's INFINITE_DEPTH, the depth that --unshallow requests.
const infiniteDepth = 0x7fffffff

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
// object-walk grafts for a fetch of wants, mirroring upstream
// send_shallow_list, deepen and send_unshallow (upload-pack.c). It returns a
// nil plan when the client neither is shallow nor asks to deepen, and an
// error when deepen is combined with deepen-since or deepen-not.
func planShallow(st storage.Storer, wants []plumbing.Hash, req shallowRequest) (*shallowPlan, error) {
	revList := !req.since.IsZero() || len(req.notTips) > 0
	if req.depth > 0 && revList {
		return nil, errors.New("deepen and deepen-since (or deepen-not) cannot be used together")
	}
	if req.depth <= 0 && !revList && len(req.clientShallows) == 0 {
		return nil, nil
	}

	plan := &shallowPlan{grafts: req.clientShallows}
	var err error
	switch {
	case revList:
		plan.shallows, err = getShallowCommitsByRevList(st, wants, req.since, req.notTips)
	case req.depth > 0:
		depth := req.depth
		if depth >= infiniteDepth && len(req.clientShallows) > 0 {
			shallows, serr := st.Shallow()
			if serr != nil {
				return nil, fmt.Errorf("reading shallow commits: %w", serr)
			}
			if len(shallows) == 0 {
				// Upstream deepen (upload-pack.c): an infinite deepen from a
				// complete repository unshallows every client shallow commit,
				// reachable from the wants or not.
				plan.deepened = true
				for _, h := range req.clientShallows {
					c, cerr := object.GetCommit(st, h)
					if errors.Is(cerr, plumbing.ErrObjectNotFound) {
						continue
					}
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
			// depth by that boundary's distance from the wants, as upstream
			// get_shallow_commits does.
			cur, ferr := shallowFrontierDepth(st, wants, req.clientShallows)
			if ferr != nil {
				return nil, fmt.Errorf("computing shallow frontier depth: %w", ferr)
			}
			if cur == 0 {
				// No client shallow commit is reachable from the wants, so
				// upstream leaves the client's view unchanged.
				return plan, nil
			}
			depth += cur
		}
		plan.shallows, err = getShallowCommits(st, wants, depth)
	default:
		// Shallow lines without a deepen: the client's boundary only bounds
		// what it has.
		return plan, nil
	}
	if err != nil {
		return nil, fmt.Errorf("computing shallow commits: %w", err)
	}

	plan.deepened = true
	plan.unshallows, plan.extraWants, err = unshallowedCommits(st, wants, req.clientShallows, plan.shallows)
	if err != nil {
		return nil, fmt.Errorf("computing unshallowed commits: %w", err)
	}
	plan.grafts = slices.Concat(req.clientShallows, plan.shallows)
	clientShallows := make(map[plumbing.Hash]struct{}, len(req.clientShallows))
	for _, h := range req.clientShallows {
		clientShallows[h] = struct{}{}
	}
	// The client already records its own shallow commits; upstream
	// send_shallow skips those flagged CLIENT_SHALLOW.
	plan.shallows = slices.DeleteFunc(plan.shallows, func(h plumbing.Hash) bool {
		_, ok := clientShallows[h]
		return ok
	})
	return plan, nil
}

// objects returns the objects to pack for a plan, or for an unbounded fetch
// when p is nil.
func (p *shallowPlan) objects(st storage.Storer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	if p == nil {
		return objectsToUpload(st, wants, haves)
	}
	wants = slices.Concat(wants, p.extraWants)
	return objectsToUpload(&shallowBoundaryStorer{Storer: st, boundary: p.grafts}, wants, haves)
}

// unshallowedCommits returns the client's shallow commits that a deepen to
// boundary makes interior, and their parents. Mirrors upstream send_unshallow
// (upload-pack.c): such a commit is reachable from the wants without crossing
// the boundary, and is not on it.
func unshallowedCommits(st storage.Storer, wants, clientShallows, boundary []plumbing.Hash) (unshallows, parents []plumbing.Hash, err error) {
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

// resolveDeepenNot resolves each deepen-not argument (a ref name or an object
// id) to a commit hash, peeling annotated tags, mirroring how upstream feeds
// "--not <oid>" to rev-list (upload-pack.c send_shallow_list).
func resolveDeepenNot(st storage.Storer, refs []string) ([]plumbing.Hash, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]plumbing.Hash, 0, len(refs))
	for _, r := range refs {
		var h plumbing.Hash
		if ref, err := storer.ResolveReference(st, plumbing.ReferenceName(r)); err == nil {
			h = ref.Hash()
		} else if oid, ok := plumbing.FromHex(r); ok {
			if _, err := st.EncodedObject(plumbing.AnyObject, oid); err != nil {
				return nil, fmt.Errorf("cannot resolve deepen-not %q", r)
			}
			h = oid
		} else {
			return nil, fmt.Errorf("cannot resolve deepen-not %q", r)
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
func reachableCommits(st storage.Storer, tips []plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	seen := make(map[plumbing.Hash]struct{})
	stack := append([]plumbing.Hash(nil), tips...)
	for len(stack) > 0 {
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

// getShallowCommitsByRevList computes the shallow boundary for a deepen-since
// and/or deepen-not request, mirroring upstream's deepen_by_rev_list
// (upload-pack.c). The included set is every commit reachable from heads that is
// not older than since (when set) and not reachable from any notTips (when set);
// a commit in the set with a parent outside it is a shallow boundary.
//
// Unlike git's rev-list traversal it does not apply the date "slop" used to
// tolerate out-of-order committer timestamps, so under clock skew the boundary
// may differ by a few commits; the resulting shallow clone is still valid.
func getShallowCommitsByRevList(st storage.Storer, heads []plumbing.Hash, since time.Time, notTips []plumbing.Hash) ([]plumbing.Hash, error) {
	exclude, err := reachableCommits(st, notTips)
	if err != nil {
		return nil, err
	}

	included := make(map[plumbing.Hash]struct{})
	parents := make(map[plumbing.Hash][]plumbing.Hash)
	visited := make(map[plumbing.Hash]struct{})
	stack := append([]plumbing.Hash(nil), heads...)
	for len(stack) > 0 {
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
// annotated tag whose (peeled) target is already in objs, it adds the tag
// object and every tag object along the chain, mirroring upstream pack-objects
// --include-tag. Lightweight tags have no tag object and are skipped.
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

// shallowBoundaryStorer reports an additional set of shallow commits (the
// per-request boundary) on top of any the repository already has. revlist's
// object walk stops at shallow commits while still collecting their full trees,
// so wrapping the storer bounds a shallow fetch's packfile to the requested
// depth — the boundary commits ship complete, their ancestors are omitted —
// without the blob loss a plain have-exclusion would cause.
type shallowBoundaryStorer struct {
	storage.Storer
	boundary []plumbing.Hash
}

func (s *shallowBoundaryStorer) Shallow() ([]plumbing.Hash, error) {
	base, err := s.Storer.Shallow()
	if err != nil {
		return nil, err
	}
	if len(s.boundary) == 0 {
		return base, nil
	}
	return append(append([]plumbing.Hash(nil), base...), s.boundary...), nil
}

// commonHaves is the negotiation state of a fetch: the client's haves that
// the server also has, as upstream tracks them in have_obj and with the
// THEY_HAVE flag, and the last answer of okToGiveUp.
type commonHaves struct {
	st storage.Storer
	// wants are the commits that must reach the client's history before
	// negotiation can stop.
	wants []plumbing.Hash
	// grafts are the commits taken as parentless, upstream's registered
	// shallow commits.
	grafts map[plumbing.Hash]struct{}

	// counted holds the common haves not already implied by an earlier one,
	// upstream's have_obj.
	counted []plumbing.Hash
	// theyHave holds every common have and the parents of common commits,
	// upstream's THEY_HAVE flag.
	theyHave map[plumbing.Hash]struct{}
	// oldest is the committer date of the oldest common commit; the walk in
	// okToGiveUp does not go below it.
	oldest time.Time
	// last is the most recent common have.
	last plumbing.Hash

	// nodes holds the commits read so far, nil for a commit the server does
	// not have. Upstream keeps parsed commits in memory and clears only its
	// marks between walks; keeping them here makes a repeated walk as cheap.
	nodes map[plumbing.Hash]*commitNode
	// starts are the wants peeled to commits, oldest first. okToGiveUp fills
	// it on its first walk.
	starts []plumbing.Hash
	// checked reports that ready holds the answer of okToGiveUp for the
	// current theyHave and oldest.
	checked bool
	ready   bool
}

// newCommonHaves returns the negotiation state of a fetch of wants, before
// any have is known, in which the commits in grafts have no parents.
func newCommonHaves(st storage.Storer, wants, grafts []plumbing.Hash) *commonHaves {
	c := &commonHaves{
		st:       st,
		wants:    wants,
		grafts:   make(map[plumbing.Hash]struct{}, len(grafts)),
		theyHave: map[plumbing.Hash]struct{}{},
		nodes:    map[plumbing.Hash]*commitNode{},
	}
	for _, h := range grafts {
		c.grafts[h] = struct{}{}
	}
	return c
}

// commitNode is what the negotiation needs of a commit.
type commitNode struct {
	when time.Time
	// parents are none for a grafted commit.
	parents []plumbing.Hash
}

// remember records commit in nodes and returns its node.
func (c *commonHaves) remember(commit *object.Commit) *commitNode {
	n := &commitNode{when: commit.Committer.When, parents: commit.ParentHashes}
	if _, ok := c.grafts[commit.Hash]; ok {
		n.parents = nil
	}
	c.nodes[commit.Hash] = n
	return n
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
// error if the object cannot be read. It mirrors upstream got_oid and
// do_got_oid: the parents of a common commit count as had before the commit
// itself is checked, and the oldest common commit bounds okToGiveUp. A have
// that adds no commit to theyHave and does not move that bound, such as a
// repeated have or a blob, keeps the answer okToGiveUp last gave.
func (c *commonHaves) add(h plumbing.Hash) (bool, error) {
	obj, err := c.st.EncodedObject(plumbing.AnyObject, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	c.last = h
	isCommit := obj.Type() == plumbing.CommitObject
	if isCommit {
		commit, err := object.DecodeCommit(c.st, obj)
		if err != nil {
			return false, err
		}
		n := c.remember(commit)
		if c.oldest.IsZero() || n.when.Before(c.oldest) {
			c.oldest = n.when
			c.checked = false
		}
		for _, p := range n.parents {
			if _, ok := c.theyHave[p]; !ok {
				c.theyHave[p] = struct{}{}
				c.checked = false
			}
		}
	}
	if _, ok := c.theyHave[h]; !ok {
		c.theyHave[h] = struct{}{}
		c.counted = append(c.counted, h)
		if isCommit {
			c.checked = false
		}
	}
	return true, nil
}

// okToGiveUp reports whether every want reaches a commit the client has, so
// that negotiation can stop. It mirrors upstream ok_to_give_up and
// can_all_from_reach_with_flag (commit-reach.c): a depth-first walk per want,
// oldest want first, sharing what earlier walks in the same call learned,
// that does not descend below the oldest common commit. Upstream orders the
// wants by generation number and then by date; without a commit-graph every
// generation number is the same, so the date decides. It reports false while
// no have is common, as upstream does. A want that does not peel to a commit
// cannot be judged by ancestry and does not hold negotiation back.
//
// The answer depends only on theyHave, oldest and the wants, so it is
// returned again without a walk until add changes one of them. Otherwise it
// walks again from scratch, as upstream does for every have it lacks, but
// over the commits in nodes, so only commits no earlier walk reached are
// read from storage. It is not
// monotonic: when a commit is dated before its parent, a lower cutoff can let
// the walk from one want visit a commit before the walk from another want
// marks it, and a true answer can turn false. A walk that finds ctx done stops
// and returns ctx's error.
func (c *commonHaves) okToGiveUp(ctx context.Context) (bool, error) {
	if len(c.counted) == 0 {
		return false, nil
	}
	if c.checked {
		return c.ready, nil
	}

	if c.starts == nil {
		for _, w := range c.wants {
			if start, ok := peelToCommit(c.st, w); ok {
				c.remember(start)
				c.starts = append(c.starts, start.Hash)
			}
		}
		slices.SortStableFunc(c.starts, func(a, b plumbing.Hash) int {
			return c.nodes[a].when.Compare(c.nodes[b].when)
		})
	}

	visited := map[plumbing.Hash]struct{}{}
	reaches := map[plumbing.Hash]struct{}{}
	marked := func(h plumbing.Hash) bool {
		_, had := c.theyHave[h]
		_, r := reaches[h]
		return had || r
	}

	type frame struct {
		hash    plumbing.Hash
		parents []plumbing.Hash
	}
	for _, start := range c.starts {
		visited[start] = struct{}{}
		stack := []frame{{hash: start, parents: c.nodes[start].parents}}
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			top := stack[len(stack)-1]
			if marked(top.hash) {
				stack = stack[:len(stack)-1]
				if len(stack) > 0 {
					reaches[stack[len(stack)-1].hash] = struct{}{}
				}
				continue
			}

			// The parents are scanned from the first one each time the walk
			// returns to a commit, so a parent marked since marks the commit,
			// and the scan still goes on to walk any parent not yet visited.
			// What this visits decides what later walks skip.
			pushed := false
			for _, p := range top.parents {
				if marked(p) {
					reaches[top.hash] = struct{}{}
				}
				if _, ok := visited[p]; ok {
					continue
				}
				visited[p] = struct{}{}
				n, err := c.node(p)
				if err != nil {
					return false, err
				}
				if n == nil || n.when.Before(c.oldest) {
					continue
				}
				stack = append(stack, frame{hash: p, parents: n.parents})
				pushed = true
				break
			}
			if !pushed {
				stack = stack[:len(stack)-1]
			}
		}

		if !marked(start) {
			c.checked, c.ready = true, false
			return false, nil
		}
	}

	c.checked, c.ready = true, true
	return true, nil
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
