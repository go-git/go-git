package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ReceivePackRequest is a set of options for the ReceivePack service.
type ReceivePackRequest struct {
	GitProtocol   string
	AdvertiseRefs bool
	StatelessRPC  bool

	// Hooks are optional server-side callbacks. The zero value installs none.
	Hooks ReceivePackHooks
}

// ReceivePackHooks holds server-side callbacks for ReceivePack.
//
// These are the in-process equivalent of git's pre-receive and post-receive
// hooks. They run after the packfile has been unpacked into the storer but
// before (PreReceive) and after (PostReceive) ref updates, so a server can
// enforce branch protection, signed-commit checks, or other policy without
// reimplementing receive-pack.
type ReceivePackHooks struct {
	// PreReceive runs after the packfile is unpacked but before any ref is
	// updated. Returning a non-nil error refuses every ref with err.Error()
	// as the report-status reason; refs are not updated and PostReceive is
	// not run.
	PreReceive func(context.Context, *PreReceiveInfo) error

	// PostReceive runs after refs are updated. Any returned error is ignored
	// for transport purposes: the refs have already moved and the
	// report-status sent to the client reflects the ref-update outcome, not
	// this error. The hook itself must handle or log failures it cares about.
	PostReceive func(context.Context, *PostReceiveInfo) error
}

// PreReceiveInfo carries the inputs to a PreReceive hook.
type PreReceiveInfo struct {
	// Storer reads the proposed new state: the objects from this push are
	// already present alongside the existing repository.
	Storer storage.Storer
	// Commands are the proposed ref updates. Treat as read-only.
	Commands []*packp.Command
	// PushOptions are the client's push options (empty if none).
	PushOptions []string
	// Progress writes to the client's sideband progress channel (band 2) when
	// negotiated, or is io.Discard otherwise. Valid only during the call.
	Progress io.Writer
}

// PostReceiveInfo carries the inputs to a PostReceive hook.
type PostReceiveInfo struct {
	// Storer reads the committed repository state.
	Storer storage.Storer
	// Commands are the ref updates that were applied successfully. Refs whose
	// update failed are omitted. Treat as read-only.
	Commands []*packp.Command
	// PushOptions are the client's push options (empty if none).
	PushOptions []string
	// Progress writes to the client's sideband progress channel (band 2) when
	// negotiated, or is io.Discard otherwise. Valid only during the call.
	Progress io.Writer
}

// ReceivePack is a server command that serves the receive-pack service.
// It closes w on every return, including malformed requests and advertisement-only
// exchanges. A close error is returned only if no earlier error occurred.
// Callers that retain ownership of their streams should wrap r with [io.NopCloser]
// and w with [ioutil.WriteNopCloser].
//
// Commands may name only references under refs/ with valid syntax and safe path
// components. Refusals are reported per command when report-status is negotiated.
// A request naming the same reference twice is rejected before hooks or reference
// updates run. See ErrFunnyRefname and ErrDuplicateRefname.
// Commands execute even when report-status is not requested. Updates use the
// storer's compare-and-set operation on the resolved target. Symbolic resolution
// is separate from the update, and deletes check the old value before a separate
// removal. ReferenceStorer has no transaction covering these steps: callers must
// serialize concurrent writers if they require the entire operation to be atomic.
func ReceivePack(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	opts *ReceivePackRequest,
) (err error) {
	if w == nil {
		return fmt.Errorf("nil writer")
	}

	w = ioutil.NewContextWriteCloser(ctx, w)

	// Every exit from here on closes the writer, because the close is what ends
	// the response for the caller's transport: a return that skips it leaves a
	// client waiting on a stream that will never end. That holds for the early
	// returns too, where nothing has been written yet, and for a refused ref,
	// where the "ng <ref> <reason>" line is the response.
	//
	// The close error only surfaces when nothing else went wrong: a rejected
	// command or a malformed request describes the exchange better than a
	// failure to hang up does.
	defer func() {
		if closeErr := closeWriter(w); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	if opts == nil {
		opts = &ReceivePackRequest{}
	}

	if opts.AdvertiseRefs || !opts.StatelessRPC {
		v := ProtocolVersion(opts.GitProtocol)
		switch v {
		case protocol.V0, protocol.V1, protocol.V2:
			// version emission (if any) is handled inside AdvertiseRefs for correct
			// ordering with the HTTP smart-reply prefix when applicable.
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedVersion, v)
		}

		if err := AdvertiseRefs(ctx, st, w, ReceivePackService, opts.StatelessRPC, v); err != nil {
			return err
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
	l, _, err := pktline.PeekLine(rd)
	if err != nil {
		return err
	}

	// At this point, if we get a flush packet, it means the client
	// has nothing to send, so we can return early.
	if l == pktline.Flush {
		return nil
	}

	updreq := &packp.UpdateRequests{}
	if err := updreq.Decode(rd); err != nil {
		return err
	}

	var (
		caps         = updreq.Capabilities
		needPackfile bool
		pushOpts     packp.PushOptions
	)

	if updreq.Capabilities.Supports(capability.PushOptions) {
		if err := pushOpts.Decode(rd); err != nil {
			return fmt.Errorf("decoding push-options: %w", err)
		}
	}

	// Should we expect a packfile?
	for _, cmd := range updreq.Commands {
		if cmd.Action() != packp.Delete {
			needPackfile = true
			break
		}
	}

	// Receive the packfile
	var unpackErr error
	if needPackfile {
		unpackErr = packfile.UpdateObjectStorage(st, rd)
	}

	// Done with the request, now close the reader
	// to indicate that we are done reading from it.
	if err := r.Close(); err != nil {
		return fmt.Errorf("closing reader: %w", err)
	}

	reportStatus := caps.Supports(capability.ReportStatus) || caps.Supports(capability.ReportStatusV2)

	var (
		useSideband bool
		writer      io.Writer = w
		progress              = io.Writer(io.Discard)
	)
	if !caps.Supports(capability.NoProgress) {
		var mux *sideband.Muxer
		if caps.Supports(capability.Sideband64k) {
			mux = sideband.NewMuxer(sideband.Sideband64k, w)
		} else if caps.Supports(capability.Sideband) {
			mux = sideband.NewMuxer(sideband.Sideband, w)
		}
		if mux != nil {
			writer = mux
			progress = sidebandProgress{mux}
			useSideband = true
		}
	}

	writeCloser := ioutil.NewWriteCloser(writer, w)

	// report is how every remaining exit answers the client: the report-status,
	// then the flush that ends the sideband stream when one is in use.
	// ReportStatus.Encode writes a flush of its own, but on a sideband exchange
	// that one is muxed into band 1 along with the rest of the report, so it
	// does not terminate the stream the client is demuxing. Routing all three
	// exits through one function is what stops one of them from answering
	// without that second flush.
	report := func(unpackErr error, cmdStatus map[plumbing.ReferenceName]error) error {
		if reportStatus {
			if err := sendReportStatus(writeCloser, updreq.Commands, unpackErr, cmdStatus); err != nil {
				return err
			}
		}
		if !useSideband {
			return nil
		}
		if err := pktline.WriteFlush(w); err != nil {
			return fmt.Errorf("flushing sideband: %w", err)
		}
		return nil
	}

	if unpackErr != nil {
		// No command was attempted, so there is no per-ref outcome to give and
		// the unpack line carries the whole reason. The error still goes back
		// to the caller: writing the report successfully does not turn a failed
		// push into a successful exchange, and the two failure exits below
		// answer the same way.
		if err := report(unpackErr, nil); err != nil {
			return err
		}
		return unpackErr
	}

	// A name carried by more than one command makes the push unapplyable: the
	// commands contradict each other, and cmdStatus holds a single outcome per
	// name, so running both would report one of them and hide the other.
	//
	// git refuses such a push outright. It batches the ref updates into a
	// transaction, and a repeated name aborts the transaction with "multiple
	// updates for ref <name> not allowed", so no ref in the batch moves and
	// every command is answered "ng" — including the commands that named a
	// reference only once. Refuse the whole request the same way, rather than
	// only the duplicated name, so a push either applies as sent or not at all.
	//
	// Unlike git this runs before PreReceive. A hook is a policy gate that may
	// have side effects of its own, so it is not asked to authorise a request
	// that cannot be applied whatever it answers.
	if dup, ok := duplicateRefname(updreq.Commands); ok {
		rejected := make(map[plumbing.ReferenceName]error, len(updreq.Commands))
		for _, cmd := range updreq.Commands {
			// sendReportStatus writes Error() into the "ng <ref> <reason>"
			// line, so the reason stays the bare sentinel: the line already
			// names the ref it speaks for. Git sends "failed to update refs"
			// here; this says more, and both are opaque to a client. Which
			// name was duplicated goes to the caller instead.
			rejected[cmd.Name] = ErrDuplicateRefname
		}
		if err := report(nil, rejected); err != nil {
			return err
		}
		return fmt.Errorf("%w: %q", ErrDuplicateRefname, dup)
	}

	if opts.Hooks.PreReceive != nil {
		info := &PreReceiveInfo{
			Storer:      st,
			Commands:    updreq.Commands,
			PushOptions: pushOpts.Options,
			Progress:    progress,
		}
		if hookErr := opts.Hooks.PreReceive(ctx, info); hookErr != nil {
			rejected := make(map[plumbing.ReferenceName]error, len(updreq.Commands))
			for _, cmd := range updreq.Commands {
				rejected[cmd.Name] = hookErr
			}
			if err := report(nil, rejected); err != nil {
				return err
			}
			return hookErr
		}
	}

	var firstErr error
	cmdStatus := make(map[plumbing.ReferenceName]error)
	updateReferences(st, updreq, cmdStatus, &firstErr)

	if opts.Hooks.PostReceive != nil {
		applied := make([]*packp.Command, 0, len(updreq.Commands))
		for _, cmd := range updreq.Commands {
			if cmdStatus[cmd.Name] == nil {
				applied = append(applied, cmd)
			}
		}
		info := &PostReceiveInfo{
			Storer:      st,
			Commands:    applied,
			PushOptions: pushOpts.Options,
			Progress:    progress,
		}
		_ = opts.Hooks.PostReceive(ctx, info)
	}

	// The unpack status reports on the packfile, not on the ref updates: it is
	// "ok" here because unpackErr was handled above. Per-command failures are
	// carried by cmdStatus as "ng <ref> <reason>" lines, exactly as the
	// PreReceive rejection path does; folding firstErr into the unpack status
	// would make a client treat a single refused ref as a corrupt push.
	if err := report(nil, cmdStatus); err != nil {
		return err
	}

	return firstErr
}

type sidebandProgress struct{ mux *sideband.Muxer }

func (p sidebandProgress) Write(b []byte) (int, error) {
	return p.mux.WriteChannel(sideband.ProgressMessage, b)
}

func closeWriter(w io.WriteCloser) error {
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing writer: %w", err)
	}
	return nil
}

// sendReportStatus writes the report-status for the exchange: the unpack line,
// then one status line per command.
//
// The lines follow cmds, not the cmdStatus map. Git reports in the order the
// commands arrived and a client is entitled to pair the two up positionally,
// whereas ranging over the map orders them differently on every push. A command
// with no entry in cmdStatus was never attempted and is not reported.
//
// One line is written per command, not per distinct name, which is what keeps
// that pairing positional: git answers a name carried by two commands with two
// ng lines. Both lines say the same thing here, because a duplicated name is
// refused before any command runs and the map holds one outcome for it.
func sendReportStatus(w io.WriteCloser, cmds []*packp.Command, unpackErr error, cmdStatus map[plumbing.ReferenceName]error) error {
	rs := &packp.ReportStatus{}
	rs.UnpackStatus = "ok"
	if unpackErr != nil {
		rs.UnpackStatus = unpackErr.Error()
	}

	for _, cmd := range cmds {
		err, ok := cmdStatus[cmd.Name]
		if !ok {
			continue
		}

		msg := "ok"
		if err != nil {
			msg = err.Error()
		}
		status := &packp.CommandStatus{
			ReferenceName: cmd.Name,
			Status:        msg,
		}
		rs.CommandStatuses = append(rs.CommandStatuses, status)
	}

	if err := rs.Encode(w); err != nil {
		return err
	}

	return nil
}

// duplicateRefname returns the first reference name that more than one command
// in cmds updates, and whether there was one.
func duplicateRefname(cmds []*packp.Command) (plumbing.ReferenceName, bool) {
	seen := make(map[plumbing.ReferenceName]struct{}, len(cmds))
	for _, cmd := range cmds {
		if _, ok := seen[cmd.Name]; ok {
			return cmd.Name, true
		}
		seen[cmd.Name] = struct{}{}
	}
	return "", false
}

func setStatus(cmdStatus map[plumbing.ReferenceName]error, firstErr *error, ref plumbing.ReferenceName, err error) {
	cmdStatus[ref] = err
	if *firstErr == nil && err != nil {
		*firstErr = err
	}
}

// checkRefname returns [ErrFunnyRefname] if receive-pack must refuse a command
// naming ref, and nil if the name may reach the storer.
//
// It mirrors the gate in Git's builtin/receive-pack.c (execute_commands_non_atomic
// -> update, "only refs/... are allowed"), which refuses a command whose name
// is not under refs/ or fails check_refname_format, reporting "funny refname".
// Without it, a push can name HEAD, CONFIG, INDEX or SHALLOW and reach the
// storer: writing HEAD repoints the repository's default branch for every
// later clone on any filesystem, and on a case-insensitive one the shouting
// names land on .git/config, .git/index and .git/shallow. A Delete command
// needs no packfile at all, so the same names also give an unauthenticated
// "remove .git/config" primitive.
//
// The name is checked in four steps:
//
//   - the refs/ prefix, which is what stops HEAD and every other root ref.
//     Git relaxes its format check for deletes (REFNAME_ALLOW_ONELEVEL) but
//     never relaxes this prefix, and neither do we: deleting a ref is the
//     cheapest form of this attack, not the most benign.
//   - ReferenceName.IsSafe, Git's refname_is_safe, for names that escape the
//     refs/ sub-tree or alias another path once joined.
//   - pathutil.HasUnsafeComponent, for the escapes IsSafe's literal ".."
//     comparison misses: control characters, and the components an HFS+ or
//     NTFS filesystem folds back to "." or "..". The dotgit storage layer
//     applies the same helper, but this gate cannot lean on it: ReceivePack is
//     exported and can be handed any storer, including one that never reaches
//     a filesystem.
//   - ReferenceName.Validate, go-git's check_refname_format, for the remaining
//     character and component rules.
//
// Three of the four are decisive somewhere. The refs/ prefix is the only thing
// that refuses HEAD, which IsSafe accepts, HasUnsafeComponent passes, and
// Validate carves out by name. IsSafe is the exception: with the prefix already
// required, every name it rejects is one Validate also rejects, by rules 1, 3,
// 6 and 10. It stays because this gate should not depend on that overlap
// holding as either function changes.
//
// Validating the full name, rather than Git's suffix after refs/, has two
// compatibility differences:
//
//   - Git runs check_refname_format on the part after "refs/" and passes
//     REFNAME_ALLOW_ONELEVEL only for deletes, so it refuses to *create*
//     refs/stash ("funny refname") while allowing it to be deleted. go-git
//     validates the whole name, which accepts one level under refs/ for every
//     action. refs/stash is a first-class ref here, and a single component
//     under refs/ cannot escape the sub-tree, so the asymmetry would cost
//     compatibility and buy no safety.
//   - For refs/@, Git tests the suffix "@" and rejects it for every action.
//     go-git accepts it because "@" is not the entire reference name. This
//     preserves the existing support for names accepted by Validate.
//
// An additional filesystem-safety restriction applies to every action:
//
//   - HasUnsafeComponent refuses a component whose first non-ignorable code
//     point is a lone ".", which check_refname_format accepts:
//     refs/heads/<U+200C>./x is "ok" to Git and "funny refname" here. On HFS+
//     that component normalises away and the name lands on refs/heads/x, which
//     is a filesystem hazard Git's format check does not model.
//
// Git relaxes only the component count for a delete and keeps every other
// format rule at its receive-pack gate. The wider
// relaxation it grants in ref_transaction_update — refname_is_safe on its own —
// is for a local caller rather than a remote one.
//
// The error is returned bare on purpose: sendReportStatus writes Error()
// verbatim into the "ng <ref> <reason>" line, so wrapping it with extra context
// would hand the client a status git never sends.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/receive-pack.c#L1491-L1499
func checkRefname(ref plumbing.ReferenceName) error {
	if !ref.IsUnderRefs() {
		return ErrFunnyRefname
	}

	if !ref.IsSafe() {
		return ErrFunnyRefname
	}

	if pathutil.HasUnsafeComponent(ref.String()) {
		return ErrFunnyRefname
	}

	if err := ref.Validate(); err != nil {
		return ErrFunnyRefname
	}

	return nil
}

func updateReferences(st storage.Storer, req *packp.UpdateRequests, cmdStatus map[plumbing.ReferenceName]error, firstErr *error) {
	for _, cmd := range req.Commands {
		if err := checkRefname(cmd.Name); err != nil {
			setStatus(cmdStatus, firstErr, cmd.Name, err)
			continue
		}

		var current *plumbing.Reference
		var err error
		if cmd.Action() == packp.Create {
			// An existing symbolic ref still occupies the name when its
			// target is missing. A create must not overwrite that alias.
			current, err = st.Reference(cmd.Name)
		} else {
			current, err = storer.ResolveReference(st, cmd.Name)
		}
		exists := err == nil
		if err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
			setStatus(cmdStatus, firstErr, cmd.Name, err)
			continue
		}

		switch cmd.Action() {
		case packp.Create:
			if exists {
				setStatus(cmdStatus, firstErr, cmd.Name, ErrUpdateReference)
				continue
			}

			ref := plumbing.NewHashReference(cmd.Name, cmd.New)
			err := st.SetReference(ref)
			setStatus(cmdStatus, firstErr, cmd.Name, err)
		case packp.Delete:
			if !exists {
				setStatus(cmdStatus, firstErr, cmd.Name, ErrUpdateReference)
				continue
			}

			if current.Hash() != cmd.Old {
				// Git permits removal of a corrupt ref when the supplied old
				// object is missing. A present old object must match the ref.
				// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/receive-pack.c#L1604-L1619.
				_, objectErr := st.EncodedObject(plumbing.AnyObject, cmd.Old)
				if objectErr == nil {
					setStatus(cmdStatus, firstErr, cmd.Name, storage.ErrReferenceHasChanged)
					continue
				}
				if !errors.Is(objectErr, plumbing.ErrObjectNotFound) {
					setStatus(cmdStatus, firstErr, cmd.Name, objectErr)
					continue
				}
			}

			err := st.RemoveReference(current.Name())
			setStatus(cmdStatus, firstErr, cmd.Name, err)
		case packp.Update:
			if !exists {
				setStatus(cmdStatus, firstErr, cmd.Name, ErrUpdateReference)
				continue
			}

			ref := plumbing.NewHashReference(current.Name(), cmd.New)
			old := plumbing.NewHashReference(current.Name(), cmd.Old)
			err := st.CheckAndSetReference(ref, old)
			setStatus(cmdStatus, firstErr, cmd.Name, err)
		}
	}
}
