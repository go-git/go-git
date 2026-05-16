package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
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
			// V0/V1 share the same classic negotiation format after the
			// (optional) version line. We only branch for V2 below.
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedVersion, v)
		}

		if err := AdvertiseRefs(ctx, st, w, UploadPackService, opts.StatelessRPC, v); err != nil {
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
		writer = pktline.NewSidebandWriter(w, pktline.MaxSize)
		useSideband = true
	} else if caps.Supports(capability.Sideband) {
		writer = pktline.NewSidebandWriter(w, pktline.DefaultSize)
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

// serveUploadPackV2 handles the git protocol v2 for upload-pack (fetch/ls-refs).
// It is used when the client requests version=2 via GIT_PROTOCOL.
func serveUploadPackV2(ctx context.Context, st storage.Storer, rd *bufio.Reader, w io.WriteCloser, opts *UploadPackRequest) error {
	for {
		cmd, kvs, args, err := readV2Request(rd)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if cmd == "" {
			// flush ended the request
			return nil
		}
		switch cmd {
		case "ls-refs":
			if err := serveLsRefsV2(ctx, st, w, kvs, args); err != nil {
				return err
			}
			// For stateless (HTTP) a single command per request; for stateful may continue but clients typically close.
			if opts.StatelessRPC {
				return nil
			}
		case "fetch":
			return serveFetchV2(ctx, st, w, rd, kvs, args, opts)
		default:
			_, _ = pktline.Writef(w, "error unknown-command %s\n", cmd)
			_ = pktline.WriteFlush(w)
			return fmt.Errorf("unsupported v2 command %q", cmd)
		}
	}
}

// readV2Request reads a v2 command request consisting of:
//   - command=xxx
//   - zero or more key=value or agent=... header lines
//   - delim (0001)
//   - zero or more argument lines (e.g. "want <oid>", "have <oid>", "done", "peel")
//   - flush (0000)
//
// It returns the command name, header kvs (as "k=v" or raw), and the post-delim args.
func readV2Request(rd *bufio.Reader) (cmd string, kvs, args []string, err error) {
	seenDelim := false
	for {
		l, line, rerr := pktline.ReadLine(rd)
		if rerr != nil {
			err = rerr
			return cmd, kvs, args, err
		}
		if l == pktline.Flush {
			return cmd, kvs, args, nil
		}
		if l == pktline.Delim {
			seenDelim = true
			continue
		}
		s := strings.TrimSuffix(string(line), "\n")
		if s == "" {
			continue
		}
		if !seenDelim {
			if after, ok := strings.CutPrefix(s, "command="); ok {
				cmd = after
				continue
			}
			// header line (agent, object-format, etc)
			kvs = append(kvs, s)
			continue
		}
		// after delim: args
		args = append(args, s)
	}
}

// serveLsRefsV2 responds to a ls-refs command.
func serveLsRefsV2(_ context.Context, st storage.Storer, w io.Writer, kvs, args []string) error {
	_ = kvs // unused for now; could check object-format
	peel := false
	symrefs := false
	// unborn not directly used for listing
	prefixes := []string{}
	for _, a := range args {
		switch a {
		case "peel":
			peel = true
		case "symrefs":
			symrefs = true
		default:
			if after, ok := strings.CutPrefix(a, "ref-prefix "); ok {
				prefixes = append(prefixes, after)
			}
		}
	}

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

	// HEAD is emitted first, but only when it passes the ref-prefix filter,
	// matching upstream's send_possibly_unborn_head -> send_ref (ls-refs.c),
	// where HEAD is subject to ref_match like every other ref.
	for _, r := range refs {
		if r.Name() == plumbing.HEAD {
			if len(prefixes) == 0 || refMatchesAnyPrefix(r.Name().String(), prefixes) {
				if err := writeV2Ref(w, st, r, symrefs, peel); err != nil {
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
		if err := writeV2Ref(w, st, r, symrefs, peel); err != nil {
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

// serveFetchV2 handles command=fetch for v2.
func serveFetchV2(_ context.Context, st storage.Storer, w io.WriteCloser, _ *bufio.Reader, kvs, args []string, opts *UploadPackRequest) error {
	_ = kvs
	var wants, haves, clientShallows []plumbing.Hash
	depth := 0
	done := false
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "want "):
			if h := plumbing.NewHash(a[5:]); !h.IsZero() {
				wants = append(wants, h)
			}
		case strings.HasPrefix(a, "have "):
			if h := plumbing.NewHash(a[5:]); !h.IsZero() {
				haves = append(haves, h)
			}
		case strings.HasPrefix(a, "shallow "):
			// The client's current shallow boundary; used to scope unshallows.
			if h := plumbing.NewHash(a[8:]); !h.IsZero() {
				clientShallows = append(clientShallows, h)
			}
		case strings.HasPrefix(a, "deepen "):
			if _, err := fmt.Sscanf(a, "deepen %d", &depth); err != nil {
				_ = w.Close()
				return fmt.Errorf("parsing deepen argument %q: %w", a, err)
			}
		case a == "deepen-relative", strings.HasPrefix(a, "deepen-since "), strings.HasPrefix(a, "deepen-not "):
			// Advertised under "shallow" but not implemented yet, as in v0/v1.
			// Fail loudly rather than silently ignore an advertised feature.
			_ = w.Close()
			return fmt.Errorf("unsupported deepen argument: %q", a)
		case a == "done":
			done = true
		}
	}

	// No 'want' lines: the client guessed it didn't want anything. Upstream
	// emits no response at all here (upload-pack.c, UPLOAD_DONE), so write
	// nothing and just close the stream — no stray flush packet.
	if len(wants) == 0 {
		return w.Close()
	}

	// Negotiation (acknowledgments section), per gitprotocol-v2 "fetch":
	//
	//   - done            -> no acknowledgments section; packfile follows.
	//   - no haves        -> clone-like; no acknowledgments section; packfile follows.
	//   - haves and !done -> emit an acknowledgments section. ACK every common
	//                        object. "ready" is sent only once every want is
	//                        reachable from the common haves (upstream's
	//                        ok_to_give_up); then the section ends with a
	//                        delim-pkt and the packfile follows. Otherwise the
	//                        section ends with a flush-pkt and NO packfile, and
	//                        the client negotiates again with more haves (NAK
	//                        when there is no common object at all).
	if !done && len(haves) > 0 {
		if _, err := pktline.Writef(w, "acknowledgments\n"); err != nil {
			return err
		}
		var common []plumbing.Hash
		for _, h := range haves {
			if _, err := st.EncodedObject(plumbing.AnyObject, h); err == nil {
				common = append(common, h)
				if _, err := pktline.Writef(w, "ACK %s\n", h); err != nil {
					return err
				}
			}
		}
		if len(common) == 0 {
			// No common object at all: NAK and end the section with a flush. No
			// packfile this round; the client negotiates again with more haves.
			if _, err := pktline.Writef(w, "NAK\n"); err != nil {
				return err
			}
			if err := pktline.WriteFlush(w); err != nil {
				return err
			}
			return w.Close()
		}
		// "ready" is withheld until every want is reachable from the common
		// haves (upstream's ok_to_give_up). Declaring it on the first common
		// have would force single-round negotiation and a larger pack. When not
		// ready, the ACK'd commons stand but the section ends with a flush so
		// the client can refine its haves in the next request.
		if !wantsReachableFromHaves(st, wants, common) {
			if err := pktline.WriteFlush(w); err != nil {
				return err
			}
			return w.Close()
		}
		// Ready: separate the acknowledgments section from the packfile section
		// with a delim-pkt (the packfile follows in the same response).
		if _, err := pktline.Writef(w, "ready\n"); err != nil {
			return err
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	// shallow-info section, emitted before the packfile when the client
	// requested a shallow fetch (e.g. clone --depth N), reusing the v0/v1
	// boundary computation. The boundary also bounds the packfile to that
	// depth (see shallowBoundaryStorer).
	//
	// Note: deepening an already-shallow clone (the client sends its own
	// "shallow" lines plus haves) is not handled — the haves would exclude the
	// deepened ancestors. Only fresh shallow fetches are bounded here.
	packSt := st
	if depth > 0 {
		var shupd packp.ShallowUpdate
		if err := getShallowCommits(st, wants, depth, &shupd); err != nil {
			_ = w.Close()
			return fmt.Errorf("computing shallow commits: %w", err)
		}
		if err := writeV2ShallowInfo(w, &shupd, clientShallows); err != nil {
			return err
		}
		packSt = &shallowBoundaryStorer{Storer: st, boundary: shupd.Shallows}
	}

	// Compute what to send
	objs, err := objectsToUpload(packSt, wants, haves)
	if err != nil {
		_ = w.Close()
		return fmt.Errorf("getting objects to upload: %w", err)
	}

	// Write packfile section header
	if _, err := pktline.Writef(w, "packfile\n"); err != nil {
		return err
	}

	// Send pack data wrapped in sideband (client switches to sideband demux after
	// seeing the "packfile" marker in v2 fetch response). Matches reference git wire.
	// Protocol v2 always negotiates sideband-64k.
	writer := pktline.NewSidebandWriter(w, pktline.MaxSize)

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
		return fmt.Errorf("encoding packfile: %w", err)
	}

	// Terminate sideband stream and v2 fetch response.
	if err := pktline.WriteFlush(w); err != nil {
		return err
	}

	return w.Close()
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

// writeV2ShallowInfo emits the protocol v2 fetch "shallow-info" section: the
// header, a "shallow <oid>" line per boundary commit and an "unshallow <oid>"
// line per previously-shallow client commit that is now complete, terminated
// by a delim-pkt separating it from the packfile section (upstream
// send_shallow_info). Unshallows are scoped to the client's reported shallow
// set, so a fresh clone never receives spurious unshallow lines.
func writeV2ShallowInfo(w io.Writer, upd *packp.ShallowUpdate, clientShallows []plumbing.Hash) error {
	if _, err := pktline.Writef(w, "shallow-info\n"); err != nil {
		return err
	}
	for _, h := range upd.Shallows {
		if _, err := pktline.Writef(w, "shallow %s\n", h); err != nil {
			return err
		}
	}
	if len(clientShallows) > 0 {
		client := make(map[plumbing.Hash]struct{}, len(clientShallows))
		for _, h := range clientShallows {
			client[h] = struct{}{}
		}
		for _, h := range upd.Unshallows {
			if _, ok := client[h]; ok {
				if _, err := pktline.Writef(w, "unshallow %s\n", h); err != nil {
					return err
				}
			}
		}
	}
	return pktline.WriteDelim(w)
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
