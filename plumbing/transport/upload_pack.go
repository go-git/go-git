package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/config"
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
	var havesWithRef map[plumbing.Hash][]plumbing.Hash
	var multiAck, multiAckDetailed bool
	var caps capability.List
	var wants []plumbing.Hash
	var ack packp.ACK
	firstRound := true
	for !done {
		writec := make(chan error)
		if firstRound || opts.StatelessRPC {
			upreq = &packp.UploadRequest{}
			if err := upreq.Decode(rd); err != nil {
				return fmt.Errorf("decoding upload-request: %w", err)
			}

			wants = upreq.Wants
			caps = upreq.Capabilities

			if err := r.Close(); err != nil {
				return fmt.Errorf("closing reader: %w", err)
			}

			// Find common commits/objects
			havesWithRef, err = revlist.ObjectsWithRef(st, wants, nil)
			if err != nil {
				return fmt.Errorf("getting objects with ref: %w", err)
			}

			// Encode objects to packfile and write to client
			multiAck = caps.Supports(capability.MultiACK)
			multiAckDetailed = caps.Supports(capability.MultiACKDetailed)

			go func() {
				// TODO: support deepen-since, and deepen-not
				var shupd packp.ShallowUpdate
				if !upreq.Depth.IsZero() {
					if upreq.Depth.Deepen > 0 {
						if err := getShallowCommits(st, wants, upreq.Depth.Deepen, &shupd); err != nil {
							writec <- fmt.Errorf("getting shallow commits: %w", err)
							return
						}
					} else {
						writec <- fmt.Errorf("unsupported depth: %+v", upreq.Depth)
						return
					}

					if err := shupd.Encode(w); err != nil {
						writec <- fmt.Errorf("sending shallow-update: %w", err)
						return
					}
				}

				writec <- nil
			}()

			if err := <-writec; err != nil {
				return err
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

		var acks []packp.ACK
		for _, hu := range uphav.Haves {
			_, ok := havesWithRef[hu]

			var status packp.ACKStatus
			if multiAckDetailed {
				status = packp.ACKCommon
				if !ok {
					status = packp.ACKReady
				}
			} else if multiAck {
				status = packp.ACKContinue
			}

			if ok || multiAck || multiAckDetailed {
				ack = packp.ACK{Hash: hu, Status: status}
				acks = append(acks, ack)
				if !multiAck && !multiAckDetailed {
					break
				}
			}
		}

		go func() {
			defer close(writec)

			if len(haves) > 0 {
				// Encode ACKs to client when we have haves
				srvrsp := packp.ServerResponse{ACKs: acks}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending acks server-response: %w", err)
					return
				}
			}

			switch {
			case !done:
				if multiAck || multiAckDetailed {
					// Encode a NAK for multi-ack
					srvrsp := packp.ServerResponse{}
					if err := srvrsp.Encode(w); err != nil {
						writec <- fmt.Errorf("sending nak server-response: %w", err)
						return
					}
				}
			case !ack.Hash.IsZero() && (multiAck || multiAckDetailed):
				// We're done, send the final ACK
				ack.Status = 0
				srvrsp := packp.ServerResponse{ACKs: []packp.ACK{ack}}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending final ack server-response: %w", err)
					return
				}
			case ack.Hash.IsZero() && len(haves) == 0:
				// No haves were sent. Emit the single terminal NAK.
				//
				// When haves *were* sent, the ServerResponse{ACKs: acks}
				// write above already emitted a NAK (encodeServerResponse
				// writes NAK when ACKs is empty). Emitting another one here
				// would produce two consecutive "0008NAK\n" pktlines;
				// ServerResponse.Decode consumes only the first, and the
				// second would then be misread by the sideband demuxer as
				// a frame with channel byte 'N' ("unknown channel NAK").
				srvrsp := packp.ServerResponse{}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending final nak server-response: %w", err)
					return
				}
			}

			writec <- nil
		}()

		if err := <-writec; err != nil {
			return err
		}

		firstRound = false
	}

	// Done with the request, now close the reader
	// to indicate that we are done reading from it.
	if err := r.Close(); err != nil {
		return fmt.Errorf("closing reader: %w", err)
	}

	objs, err := objectsToUpload(st, wants, haves)
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

func getShallowCommits(st storage.Storer, heads []plumbing.Hash, depth int, upd *packp.ShallowUpdate) error {
	var i, curDepth int
	var commit *object.Commit
	depths := map[*object.Commit]int{}
	stack := []object.Object{}

	for commit != nil || i < len(heads) || len(stack) > 0 {
		if commit == nil {
			if i < len(heads) {
				obj, err := st.EncodedObject(plumbing.CommitObject, heads[i])
				i++
				if err != nil {
					continue
				}

				commit, err = object.DecodeCommit(st, obj)
				if err != nil {
					commit = nil
					continue
				}

				depths[commit] = 0
				curDepth = 0
			} else if len(stack) > 0 {
				commit = stack[len(stack)-1].(*object.Commit)
				stack = stack[:len(stack)-1]
				curDepth = depths[commit]
			}
		}

		curDepth++

		if depth != math.MaxInt && curDepth >= depth {
			upd.Shallows = append(upd.Shallows, commit.Hash)
			commit = nil
			continue
		}

		upd.Unshallows = append(upd.Unshallows, commit.Hash)

		parents := commit.Parents()
		commit = nil
		for {
			parent, err := parents.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			if depths[parent] != 0 && curDepth >= depths[parent] {
				continue
			}

			depths[parent] = curDepth

			if _, err := parents.Next(); err == nil {
				stack = append(stack, parent)
			} else {
				commit = parent
				curDepth = depths[commit]
			}
		}
	}

	return nil
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
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		refs = append(refs, r)
		return nil
	})

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

func writeV2Ref(w io.Writer, st storage.Storer, r *plumbing.Reference, symrefs, peel bool) error {
	var hash plumbing.Hash
	var target string
	if r.Type() == plumbing.SymbolicReference {
		ref, err := storer.ResolveReference(st, r.Target())
		if err == nil {
			hash = ref.Hash()
		}
		target = r.Target().String()
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
	if symrefs && target != "" {
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
func serveFetchV2(_ context.Context, st storage.Storer, w io.WriteCloser, args *packp.FetchArgs, opts *UploadPackRequest) (concluded bool, err error) {
	wants := args.Wants
	haves := args.Haves
	clientShallows := args.Shallows
	depth := args.Deepen
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
	//                        object. "ready" is sent only once every want is
	//                        reachable from the common haves (upstream's
	//                        ok_to_give_up); then the packfile follows in the
	//                        same response. Otherwise the section ends without a
	//                        packfile and the client negotiates again with more
	//                        haves (NAK when there is no common object at all).
	if !done && len(haves) > 0 {
		var common []plumbing.Hash
		for _, h := range haves {
			if _, err := st.EncodedObject(plumbing.AnyObject, h); err == nil {
				common = append(common, h)
			}
		}
		out.Acknowledgments = &packp.Acknowledgments{ACKs: common}

		// "ready" is withheld until every want is reachable from the common
		// haves (upstream's ok_to_give_up). Declaring it on the first common
		// have would force single-round negotiation and a larger pack. When not
		// ready (including no common object at all, which encodes as NAK), the
		// acknowledgments section stands alone and the client refines its haves
		// in the next request.
		if len(common) == 0 || !wantsReachableFromHaves(st, wants, common) {
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

	// shallow-info: a shallow fetch bounds the history sent. The boundary forms
	// mirror upstream send_shallow_list (upload-pack.c):
	//   - deepen <n>: a depth boundary from the wants (getShallowCommits).
	//   - deepen-since / deepen-not: a date/ref boundary (getShallowCommitsByRevList,
	//     mirroring deepen_by_rev_list).
	// Upstream forbids combining deepen with deepen-since/deepen-not, and so do we.
	// deepen-relative only changes how the depth is counted: for a fresh fetch
	// (no client shallows) relative and absolute depth coincide, and for an
	// already-shallow client the depth is offset by the existing boundary's
	// distance from the wants (see the deepen-relative handling below).
	since := args.DeepenSince
	notTips, err := resolveDeepenNot(st, args.DeepenNot)
	if err != nil {
		_ = w.Close()
		return true, fmt.Errorf("resolving deepen-not: %w", err)
	}
	revList := !since.IsZero() || len(notTips) > 0
	if depth > 0 && revList {
		_ = w.Close()
		return true, fmt.Errorf("deepen and deepen-since (or deepen-not) cannot be used together")
	}

	// A deepen was requested when depth > 0 or a rev-list bound was given.
	// haveNewBoundary records that separately from len(newBoundary): a deepen
	// that reaches full history yields an empty boundary, which still drives
	// shallow-info and the unshallow lines and must not be mistaken for "no
	// deepen requested". newBoundary is the grafting boundary for the deepened
	// view (nil/empty means graft nothing: full history).
	var newBoundary []plumbing.Hash
	var haveNewBoundary bool
	if depth > 0 || revList {
		var shupd packp.ShallowUpdate
		computed := true
		if revList {
			err = getShallowCommitsByRevList(st, wants, since, notTips, &shupd)
		} else {
			effectiveDepth := depth
			if args.DeepenRelative && len(clientShallows) > 0 {
				// deepen-relative counts depth from the client's existing
				// shallow boundary, not from the wants. Mirror upstream
				// get_shallow_commits (shallow.c): offset the absolute depth by
				// the depth at which that boundary sits from the wants.
				cur, derr := shallowFrontierDepth(st, wants, clientShallows)
				if derr != nil {
					_ = w.Close()
					return true, fmt.Errorf("computing shallow frontier depth: %w", derr)
				}
				if cur == 0 {
					// No client shallow is reachable from the wants; upstream
					// computes no new boundary and leaves the client's view
					// unchanged. Skip the deepen entirely.
					computed = false
				} else {
					effectiveDepth = depth + cur
				}
			}
			if computed {
				err = getShallowCommits(st, wants, effectiveDepth, &shupd)
			}
		}
		if err != nil {
			_ = w.Close()
			return true, fmt.Errorf("computing shallow commits: %w", err)
		}
		if computed {
			haveNewBoundary = true
			newBoundary = shupd.Shallows
		}
	}

	var objs []plumbing.Hash
	if len(clientShallows) > 0 {
		// The client already has a shallow view (it sent "shallow" lines).
		// A single object walk cannot graft the wanted history at the new
		// boundary while also grafting the client's have-history at its existing
		// boundary, so compute two views and send their difference:
		//   newView    = objects reachable from the wants, grafted at the new
		//                boundary (the client's deepened view).
		//   clientView = objects the client already has, reachable from its haves
		//                grafted at its existing shallow boundary.
		// newView \ clientView is exactly what the client is missing. It never
		// omits a needed object; at worst it re-sends one the client has, which
		// is harmless. This is what bounds a deepen of an already-shallow clone.
		boundary := clientShallows
		if haveNewBoundary {
			// The deepened boundary, which may be empty: a deepen that reaches
			// full history grafts nothing and unshallows the old boundary.
			boundary = newBoundary
		}
		newView, nerr := objectsToUpload(&shallowBoundaryStorer{Storer: st, boundary: boundary}, wants, nil)
		if nerr != nil {
			_ = w.Close()
			return true, fmt.Errorf("getting objects to upload: %w", nerr)
		}
		clientView, cerr := objectsToUpload(&shallowBoundaryStorer{Storer: st, boundary: clientShallows}, haves, nil)
		if cerr != nil {
			_ = w.Close()
			return true, fmt.Errorf("getting client objects: %w", cerr)
		}
		objs = hashDifference(newView, clientView)
		if haveNewBoundary {
			out.ShallowInfo = &packp.ShallowInfo{
				Shallows:   newBoundary,
				Unshallows: unshallowedCommits(clientShallows, newBoundary, newView),
			}
		}
	} else {
		packSt := st
		if haveNewBoundary && len(newBoundary) > 0 {
			out.ShallowInfo = &packp.ShallowInfo{Shallows: newBoundary}
			packSt = &shallowBoundaryStorer{Storer: st, boundary: newBoundary}
		}
		objs, err = objectsToUpload(packSt, wants, haves)
		if err != nil {
			_ = w.Close()
			return true, fmt.Errorf("getting objects to upload: %w", err)
		}
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

// hashDifference returns the elements of a that are not in b, preserving a's
// order. It computes the objects a deepened client is missing (newView minus the
// client's existing view).
func hashDifference(a, b []plumbing.Hash) []plumbing.Hash {
	set := make(map[plumbing.Hash]struct{}, len(b))
	for _, h := range b {
		set[h] = struct{}{}
	}
	var out []plumbing.Hash
	for _, h := range a {
		if _, ok := set[h]; !ok {
			out = append(out, h)
		}
	}
	return out
}

// unshallowedCommits returns the client's shallow commits that the deepened view
// now includes as interior commits (their parents are being sent), so the client
// can clear their shallow mark. Commits still on the new boundary stay shallow.
// Mirrors upstream send_unshallow (upload-pack.c).
func unshallowedCommits(clientShallows, newBoundary, newView []plumbing.Hash) []plumbing.Hash {
	inView := make(map[plumbing.Hash]struct{}, len(newView))
	for _, h := range newView {
		inView[h] = struct{}{}
	}
	boundary := make(map[plumbing.Hash]struct{}, len(newBoundary))
	for _, h := range newBoundary {
		boundary[h] = struct{}{}
	}
	var out []plumbing.Hash
	for _, cs := range clientShallows {
		if _, ok := inView[cs]; !ok {
			continue // not part of the deepened view
		}
		if _, ok := boundary[cs]; ok {
			continue // still a boundary commit
		}
		out = append(out, cs)
	}
	return out
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
func getShallowCommitsByRevList(st storage.Storer, heads []plumbing.Hash, since time.Time, notTips []plumbing.Hash, upd *packp.ShallowUpdate) error {
	exclude, err := reachableCommits(st, notTips)
	if err != nil {
		return err
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

	for h := range included {
		for _, p := range parents[h] {
			if _, ok := included[p]; !ok {
				upd.Shallows = append(upd.Shallows, h)
				break
			}
		}
	}
	plumbing.HashesSort(upd.Shallows)
	return nil
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

// wantsReachableFromHaves reports whether every want is reachable from the set
// of common haves — upstream's ok_to_give_up (upload-pack.c). A want is anchored
// when a common have is the want itself or one of its ancestors, i.e. the want
// can reach a have by walking parents. Tags are peeled to commits first, as the
// ancestry walk operates on commits. Returns false (keep negotiating) if any
// want cannot be resolved to a commit or is not yet anchored.
func wantsReachableFromHaves(st storage.Storer, wants, commonHaves []plumbing.Hash) bool {
	haveSet := make(map[plumbing.Hash]struct{}, len(commonHaves))
	haveCommits := make([]*object.Commit, 0, len(commonHaves))
	for _, h := range commonHaves {
		haveSet[h] = struct{}{}
		if c, ok := peelToCommit(st, h); ok {
			haveCommits = append(haveCommits, c)
		}
	}

	for _, wHash := range wants {
		wc, ok := peelToCommit(st, wHash)
		if !ok {
			return false
		}
		if _, ok := haveSet[wc.Hash]; ok {
			continue
		}
		anchored := false
		for _, hc := range haveCommits {
			if hc.Hash == wc.Hash {
				anchored = true
				break
			}
			if isAnc, err := hc.IsAncestor(wc); err == nil && isAnc {
				anchored = true
				break
			}
		}
		if !anchored {
			return false
		}
	}
	return true
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
