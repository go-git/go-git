package transport

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

// worktreesDir holds the administrative files of linked worktrees.
const worktreesDir = "worktrees"

// denyAction is a value of receive.denyCurrentBranch or
// receive.denyDeleteCurrent.
type denyAction int

const (
	denyUnconfigured denyAction = iota
	denyIgnore
	denyWarn
	denyRefuse
	denyUpdateInstead
)

// refuseUnconfiguredDenyMsg and refuseUnconfiguredDenyDeleteCurrentMsg are
// the advice git sends when it refuses a command only because the matching
// setting is absent.
const (
	refuseUnconfiguredDenyMsg = "By default, updating the current branch in a non-bare repository\n" +
		"is denied, because it will make the index and work tree inconsistent\n" +
		"with what you pushed, and will require 'git reset --hard' to match\n" +
		"the work tree to HEAD.\n" +
		"\n" +
		"You can set the 'receive.denyCurrentBranch' configuration variable\n" +
		"to 'ignore' or 'warn' in the remote repository to allow pushing into\n" +
		"its current branch; however, this is not recommended unless you\n" +
		"arranged to update its work tree to match what you pushed in some\n" +
		"other way.\n" +
		"\n" +
		"To squelch this message and still keep the default behaviour, set\n" +
		"'receive.denyCurrentBranch' configuration variable to 'refuse'."

	refuseUnconfiguredDenyDeleteCurrentMsg = "By default, deleting the current branch is denied, because the next\n" +
		"'git clone' won't result in any file checked out, causing confusion.\n" +
		"\n" +
		"You can set 'receive.denyDeleteCurrent' configuration variable to\n" +
		"'warn' or 'ignore' in the remote repository to allow deleting the\n" +
		"current branch, with or without a warning message.\n" +
		"\n" +
		"To squelch this message, you can set it to 'refuse'."
)

// receivePolicy is the part of the repository configuration that decides
// whether receive-pack lets a command update a reference. It follows
// receive_pack_config and update in git's builtin/receive-pack.c.
//
// https://github.com/git/git/blob/master/builtin/receive-pack.c
type receivePolicy struct {
	// bare is core.bare. Git treats a repository that does not set it as
	// bare when it has no work tree, which is how receive-pack opens it.
	bare                bool
	denyCurrentBranch   denyAction
	denyDeleteCurrent   denyAction
	denyNonFastForwards bool
}

// loadReceivePolicy reads the receive policy from the configuration of st.
// A value git cannot parse is an error, as git dies on it before it
// advertises any reference.
func loadReceivePolicy(st storage.Storer) (receivePolicy, error) {
	p := receivePolicy{bare: true}
	cfg, err := st.Config()
	if err != nil {
		return p, err
	}
	if cfg.Raw == nil {
		return p, nil
	}

	if v, ok := configOption(cfg.Raw, "core", "bare"); ok {
		if p.bare, err = parseConfigBool("core.bare", v); err != nil {
			return p, err
		}
	}
	if v, ok := configOption(cfg.Raw, "receive", "denyCurrentBranch"); ok {
		if p.denyCurrentBranch, err = parseDenyAction("receive.denycurrentbranch", v); err != nil {
			return p, err
		}
	}
	if v, ok := configOption(cfg.Raw, "receive", "denyDeleteCurrent"); ok {
		if p.denyDeleteCurrent, err = parseDenyAction("receive.denydeletecurrent", v); err != nil {
			return p, err
		}
	}
	if v, ok := configOption(cfg.Raw, "receive", "denyNonFastForwards"); ok {
		if p.denyNonFastForwards, err = parseConfigBool("receive.denynonfastforwards", v); err != nil {
			return p, err
		}
	}

	return p, nil
}

// configOption returns the last value of key in section without adding the
// section to cfg when it is absent. A key written without a value reads as
// "true", as git's boolean parsing takes it, while "key =" reads as empty.
func configOption(cfg *format.Config, section, key string) (string, bool) {
	if !cfg.HasSection(section) {
		return "", false
	}
	for _, o := range slices.Backward(cfg.Section(section).Options) {
		if !o.IsKey(key) {
			continue
		}
		if o.NoValue && o.Value == "" {
			return "true", true
		}
		return o.Value, true
	}
	return "", false
}

// parseDenyAction mirrors parse_deny_action: one of the named actions, in
// any case, or a boolean where true refuses and false ignores.
func parseDenyAction(key, value string) (denyAction, error) {
	switch strings.ToLower(value) {
	case "ignore":
		return denyIgnore, nil
	case "warn":
		return denyWarn, nil
	case "refuse":
		return denyRefuse, nil
	case "updateinstead":
		return denyUpdateInstead, nil
	}
	refuse, err := parseConfigBool(key, value)
	if err != nil {
		return denyUnconfigured, err
	}
	if refuse {
		return denyRefuse, nil
	}
	return denyIgnore, nil
}

// parseConfigBool mirrors git_config_bool, which accepts true, yes, on,
// false, no, off and the empty string in any case, plus any integer.
func parseConfigBool(key, value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off", "":
		return false, nil
	}
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return i != 0, nil
	}
	return false, fmt.Errorf("bad boolean config value '%s' for '%s'", value, key)
}

// checkedOut names the branches git's update in builtin/receive-pack.c
// treats as checked out, which find_shared_symref in worktree.c finds by
// looking at the HEAD of every worktree.
type checkedOut struct {
	// head is the branch the repository's HEAD names. It is checked out
	// unless the repository is bare.
	head plumbing.ReferenceName
	// linked holds the branches the HEADs of linked worktrees name. A
	// linked worktree is never bare.
	linked []plumbing.ReferenceName
}

// loadCheckedOut reads the branches checked out in st. Linked worktrees
// are found only when st exposes its filesystem.
func loadCheckedOut(st storage.Storer) checkedOut {
	c := checkedOut{head: symrefTarget(st, plumbing.HEAD)}
	if wts, ok := st.(xstorage.WorktreeStorer); ok {
		c.linked = linkedBranches(st, wts.Filesystem())
	}
	return c
}

// inWorktree reports whether name is checked out in a worktree that is not
// bare, the condition under which git applies receive.denyCurrentBranch.
func (c checkedOut) inWorktree(name plumbing.ReferenceName, bare bool) bool {
	return (!bare && name == c.head) || slices.Contains(c.linked, name)
}

// isCurrent reports whether name is checked out in any worktree or is the
// branch the repository's HEAD names even when it is bare, the condition
// under which git applies receive.denyDeleteCurrent.
func (c checkedOut) isCurrent(name plumbing.ReferenceName) bool {
	return name == c.head || slices.Contains(c.linked, name)
}

// linkedBranches returns the branches the HEADs of the linked worktrees
// recorded under worktrees in fs name. Like get_worktrees in git's
// worktree.c, it skips an entry without a gitdir file, and finds none when
// the directory cannot be read.
func linkedBranches(st storer.ReferenceStorer, fs billy.Filesystem) []plumbing.ReferenceName {
	entries, err := fs.ReadDir(worktreesDir)
	if err != nil {
		return nil
	}

	var branches []plumbing.ReferenceName
	for _, e := range entries {
		dir := fs.Join(worktreesDir, e.Name())
		gitdir, err := util.ReadFile(fs, fs.Join(dir, "gitdir"))
		if err != nil || len(gitdir) == 0 {
			continue
		}
		head, err := util.ReadFile(fs, fs.Join(dir, "HEAD"))
		if err != nil {
			continue
		}
		ref := plumbing.NewReferenceFromStrings(plumbing.HEAD.String(), strings.TrimSpace(string(head)))
		if ref.Type() != plumbing.SymbolicReference {
			continue
		}
		if name := resolveSymref(st, ref); name != "" {
			branches = append(branches, name)
		}
	}
	return branches
}

// symrefTarget returns the branch name resolves to, which need not exist
// yet. It returns an empty name when name is missing or not symbolic.
func symrefTarget(st storer.ReferenceStorer, name plumbing.ReferenceName) plumbing.ReferenceName {
	ref, err := st.Reference(name)
	if err != nil || ref.Type() != plumbing.SymbolicReference {
		return ""
	}
	return resolveSymref(st, ref)
}

// resolveSymref follows the symbolic reference ref to the last name in the
// chain. It returns an empty name when the chain is too deep.
func resolveSymref(st storer.ReferenceStorer, ref *plumbing.Reference) plumbing.ReferenceName {
	for range storer.MaxResolveRecursion {
		next, err := st.Reference(ref.Target())
		if err != nil || next.Type() != plumbing.SymbolicReference {
			return ref.Target()
		}
		ref = next
	}
	return ""
}

// checkCurrentBranch applies receive.denyCurrentBranch to cmd. It reports
// whether git would update the work tree instead, which is refused only
// after the remaining checks have run, as git defers it to the same point.
func (p receivePolicy) checkCurrentBranch(cmd *packp.Command, current checkedOut, messages io.Writer) (bool, error) {
	if !current.inWorktree(cmd.Name, p.bare) {
		return false, nil
	}

	switch p.denyCurrentBranch {
	case denyIgnore:
	case denyWarn:
		rpMessage(messages, "warning: ", "updating the current branch")
	case denyUpdateInstead:
		return true, nil
	default:
		rpMessage(messages, "error: ", "refusing to update checked out branch: "+cmd.Name.String())
		if p.denyCurrentBranch == denyUnconfigured {
			rpMessage(messages, "error: ", refuseUnconfiguredDenyMsg)
		}
		return false, ErrBranchCheckedOut
	}
	return false, nil
}

// checkUpdate applies receive.denyDeleteCurrent and
// receive.denyNonFastForwards to cmd.
func (p receivePolicy) checkUpdate(st storer.EncodedObjectStorer, cmd *packp.Command, current checkedOut, messages io.Writer) error {
	switch cmd.Action() {
	case packp.Delete:
		if !current.isCurrent(cmd.Name) {
			return nil
		}
		switch p.denyDeleteCurrent {
		case denyIgnore:
		case denyWarn:
			rpMessage(messages, "warning: ", "deleting the current branch")
		default:
			if p.denyDeleteCurrent == denyUnconfigured {
				rpMessage(messages, "error: ", refuseUnconfiguredDenyDeleteCurrentMsg)
			}
			rpMessage(messages, "error: ", "refusing to delete the current branch: "+cmd.Name.String())
			return ErrDeleteCurrentBranch
		}
	case packp.Update:
		if !p.denyNonFastForwards || !cmd.Name.IsBranch() {
			return nil
		}
		oldCommit, oldErr := object.GetCommit(st, cmd.Old)
		newCommit, newErr := object.GetCommit(st, cmd.New)
		if oldErr != nil || newErr != nil {
			return ErrBadRef
		}
		ok, err := oldCommit.IsAncestor(newCommit)
		if err != nil {
			return err
		}
		if !ok {
			rpMessage(messages, "error: ", "denying non-fast-forward "+cmd.Name.String()+" (you should pull first)")
			return ErrNonFastForward
		}
	}
	return nil
}

// rpMessage writes msg to the client the way git's rp_error and rp_warning
// do, on the progress band when side-band was negotiated.
func rpMessage(w io.Writer, prefix, msg string) {
	_, _ = io.WriteString(w, prefix+msg+"\n")
}
