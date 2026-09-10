package plumbing

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// RefPrefix is the sub-tree every ordinary reference lives under. The
	// reference store, the receive-pack gate and both ends of the transport
	// all draw a line at it, so they draw it at the same string.
	RefPrefix = "refs/"
	// RefHeadPrefix is the sub-tree branches live under.
	RefHeadPrefix = RefPrefix + "heads/"

	refTagPrefix    = RefPrefix + "tags/"
	refRemotePrefix = RefPrefix + "remotes/"
	refNotePrefix   = RefPrefix + "notes/"
	symrefPrefix    = "ref: "
)

// RefRevParseRules are a set of rules to parse references into short names, or expand into a full reference.
// These are the same rules as used by git in shorten_unambiguous_ref and expand_ref.
// See: https://github.com/git/git/blob/e0aaa1b6532cfce93d87af9bc813fb2e7a7ce9d7/refs.c#L417
var RefRevParseRules = []string{
	"%s",
	"refs/%s",
	"refs/tags/%s",
	"refs/heads/%s",
	"refs/remotes/%s",
	"refs/remotes/%s/HEAD",
}

var (
	// ErrReferenceNotFound is returned when a reference is not found.
	ErrReferenceNotFound = errors.New("reference not found")

	// ErrInvalidReferenceName is returned when a reference name is invalid.
	ErrInvalidReferenceName = errors.New("invalid reference name")
)

// ReferenceType reference type's
type ReferenceType int8

const (
	// InvalidReference represents an invalid reference type.
	InvalidReference ReferenceType = 0
	// HashReference represents a hash reference.
	HashReference ReferenceType = 1
	// SymbolicReference represents a symbolic reference.
	SymbolicReference ReferenceType = 2
)

func (r ReferenceType) String() string {
	switch r {
	case InvalidReference:
		return "invalid-reference"
	case HashReference:
		return "hash-reference"
	case SymbolicReference:
		return "symbolic-reference"
	}

	return ""
}

// ReferenceName reference name's
type ReferenceName string

// NewBranchReferenceName returns a reference name describing a branch based on
// his short name.
func NewBranchReferenceName(name string) ReferenceName {
	return ReferenceName(RefHeadPrefix + name)
}

// NewNoteReferenceName returns a reference name describing a note based on his
// short name.
func NewNoteReferenceName(name string) ReferenceName {
	return ReferenceName(refNotePrefix + name)
}

// NewRemoteReferenceName returns a reference name describing a remote branch
// based on his short name and the remote name.
func NewRemoteReferenceName(remote, name string) ReferenceName {
	return ReferenceName(refRemotePrefix + fmt.Sprintf("%s/%s", remote, name))
}

// NewRemoteHEADReferenceName returns a reference name describing a the HEAD
// branch of a remote.
func NewRemoteHEADReferenceName(remote string) ReferenceName {
	return ReferenceName(refRemotePrefix + fmt.Sprintf("%s/%s", remote, HEAD))
}

// NewTagReferenceName returns a reference name describing a tag based on short
// his name.
func NewTagReferenceName(name string) ReferenceName {
	return ReferenceName(refTagPrefix + name)
}

// IsBranch check if a reference is a branch
func (r ReferenceName) IsBranch() bool {
	return strings.HasPrefix(string(r), RefHeadPrefix)
}

// IsNote check if a reference is a note
func (r ReferenceName) IsNote() bool {
	return strings.HasPrefix(string(r), refNotePrefix)
}

// IsRemote check if a reference is a remote
func (r ReferenceName) IsRemote() bool {
	return strings.HasPrefix(string(r), refRemotePrefix)
}

// IsTag check if a reference is a tag
func (r ReferenceName) IsTag() bool {
	return strings.HasPrefix(string(r), refTagPrefix)
}

// IsPeeled returns true if the reference name ends with "^{}", indicating
// it is a peeled (dereferenced) tag. This is used in the Git protocol.
func (r ReferenceName) IsPeeled() bool {
	return strings.HasSuffix(string(r), "^{}")
}

// IsUnderRefs reports whether the name lives in the refs/ sub-tree, where
// every ordinary reference lives.
//
// Four gates draw a line here and each drew it with its own copy of the
// string: the storer deciding what it will write, the receive-pack gate
// deciding what a push may name, and the two ends of the transport deciding
// what may be advertised and what an advertisement may carry. What each does
// with the answer differs — the transport keeps HEAD, the storer also takes a
// root ref, and a push takes neither — so this reports the fact and leaves
// the policy to them.
func (r ReferenceName) IsUnderRefs() bool {
	return strings.HasPrefix(string(r), RefPrefix)
}

// IsSafe reports whether the reference name can be safely turned into a path
// under the .git directory. It follows Git's refname_is_safe, but rejects
// backslashes on every platform; Git rejects them only on Windows. A name
// is safe when it is either:
//
//   - under "refs/", non-empty after the prefix, containing no backslash and
//     no empty, "." or ".." path component (so it cannot escape the refs/
//     sub-tree, or alias another name, once turned into a path); or
//   - a one-level name whose spelling is restricted to [A-Z_]
//     (e.g. HEAD, ORIG_HEAD, FETCH_HEAD).
//
// Everything else — a lowercase or mixed one-level name such as "config" or
// "index", an absolute or drive-prefixed name, or a refs/ name that escapes —
// is unsafe.
//
// IsSafe is not an allowlist of legitimate names, and does not on its own keep
// a name off unrelated repository metadata: its one-level arm admits any [A-Z_]
// spelling, "CONFIG" and "SHALLOW" included. A caller that creates or updates a
// reference must also require the name to be under refs/ or to satisfy IsRoot.
// This is a storage-safety check, not full check_refname_format validation; see
// Validate for the latter.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs.c#L382-L412
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs/refs-internal.h#L57-L69
func (r ReferenceName) IsSafe() bool {
	s := string(r)
	if s == "" {
		return false
	}

	if rest, ok := strings.CutPrefix(s, RefPrefix); ok {
		// '\' is a path separator on Windows, so a refs/ name containing one
		// could escape the sub-tree or alias another name once turned into a
		// path; reject it outright (check_refname_format forbids '\' too).
		if rest == "" || strings.Contains(rest, "\\") {
			return false
		}
		for part := range strings.SplitSeq(rest, "/") {
			if part == "" || part == "." || part == ".." {
				return false
			}
		}
		return true
	}

	// Git's refname_is_safe admits any [A-Z_] one-level spelling.
	// "CONFIG" and "SHALLOW" fold onto .git/config and .git/shallow on a
	// case-insensitive filesystem; IsRoot is what tells the genuine root refs
	// apart from those.
	for i := 0; i < len(s); i++ {
		if (s[i] < 'A' || s[i] > 'Z') && s[i] != '_' {
			return false
		}
	}
	return true
}

// IsRoot reports whether the reference name is one of the one-level references
// that legitimately live in the root of the reference store, next to refs/.
// It mirrors Git's is_root_ref (refs.c): the name must be spelled with
// [A-Z_-] only and must either end in "_HEAD" (ORIG_HEAD, FETCH_HEAD,
// MERGE_HEAD, CHERRY_PICK_HEAD, REBASE_HEAD, ...) or be one of the irregular
// names Git lists explicitly (HEAD, AUTO_MERGE, BISECT_EXPECTED_REV,
// NOTES_MERGE_PARTIAL, NOTES_MERGE_REF, MERGE_AUTOSTASH). Unlike Git's
// is_root_ref, FETCH_HEAD and MERGE_HEAD are included.
//
// IsRoot reports false for every name under refs/, and is not a complete test
// on its own: its alphabet admits '-', so "SOME-THING_HEAD" satisfies IsRoot
// while IsSafe rejects it. Test IsSafe first, then accept the name if it is
// under refs/ or satisfies IsRoot.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs.c#L887-L938
func (r ReferenceName) IsRoot() bool {
	// An allowlist by design: denying the names of known .git entries instead
	// would be incomplete the moment Git adds a file, whereas the set of
	// legitimate root refs changes only when Git grows a new one.
	//
	// FETCH_HEAD and MERGE_HEAD are kept because Git excludes them only via
	// is_pseudo_ref, which marks the names its ref transactions refuse to
	// update since they carry more than an object id. That is a write-policy
	// rule rather than a naming rule. This predicate describes spelling and
	// permits callers to store those names directly.
	s := string(r)
	if s == "" {
		return false
	}

	// is_root_ref_syntax: uppercase, '-' and '_' only.
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 'A' || c > 'Z') && c != '_' && c != '-' {
			return false
		}
	}

	if strings.HasSuffix(s, "_HEAD") {
		return true
	}

	// The one-level names is_root_ref accepts by exact spelling, i.e. those
	// that do not match its "*_HEAD" suffix rule.
	switch r {
	case "HEAD", "AUTO_MERGE", "BISECT_EXPECTED_REV",
		"NOTES_MERGE_PARTIAL", "NOTES_MERGE_REF", "MERGE_AUTOSTASH":
		return true
	}

	return false
}

func (r ReferenceName) String() string {
	return string(r)
}

// Short returns the short name of a ReferenceName
func (r ReferenceName) Short() string {
	s := string(r)
	res := s
	for _, format := range RefRevParseRules[1:] {
		_, err := fmt.Sscanf(s, format, &res)
		if err == nil {
			continue
		}
	}

	return res
}

var ctrlSeqs = regexp.MustCompile(`[\000-\037\177]`)

// Validate validates a reference name.
// This follows the git-check-ref-format rules.
// See https://git-scm.com/docs/git-check-ref-format
//
// It is important to note that this function does not check if the reference
// exists in the repository.
// It only checks if the reference name is valid.
// This functions does not support the --refspec-pattern, --normalize, and
// --allow-onelevel options.
//
// Git imposes the following rules on how references are named:
//
//  1. They can include slash / for hierarchical (directory) grouping, but no
//     slash-separated component can begin with a dot . or end with the
//     sequence .lock.
//  2. They must contain at least one /. This enforces the presence of a
//     category like heads/, tags/ etc. but the actual names are not
//     restricted. If the --allow-onelevel option is used, this rule is
//     waived.
//  3. They cannot have two consecutive dots .. anywhere.
//  4. They cannot have ASCII control characters (i.e. bytes whose values are
//     lower than \040, or \177 DEL), space, tilde ~, caret ^, or colon :
//     anywhere.
//  5. They cannot have question-mark ?, asterisk *, or open bracket [
//     anywhere. See the --refspec-pattern option below for an exception to this
//     rule.
//  6. They cannot begin or end with a slash / or contain multiple consecutive
//     slashes (see the --normalize option below for an exception to this rule).
//  7. They cannot end with a dot ..
//  8. They cannot contain a sequence @{.
//  9. They cannot be the single character @.
//  10. They cannot contain a \.
//
// A leading "-" is not among them, and neither is a component spelled "@".
// Git restricts a leading "-" when a branch or a tag is created, in
// check_branch_ref and check_tag_ref rather than in check_refname_format; see
// ValidateBranchName and ValidateTagName.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs.c#L191-L322
func (r ReferenceName) Validate() error {
	s := string(r)
	if len(s) == 0 {
		return fmt.Errorf("%w: %q", ErrInvalidReferenceName, s)
	}

	// HEAD is a special case
	if r == HEAD {
		return nil
	}

	// rule 9. The rule is about the whole name, not about a component: Git
	// tests it once, against the entire string, before it starts walking the
	// components (check_or_sanitize_refname, refs.c). "@" is an ordinary
	// character everywhere else, so "refs/heads/@" is a name git branch
	// creates and git clone replicates.
	//
	// Rule 2 below already refuses every one-level name, so this changes no
	// outcome today. It is spelled out because Git needs it — the rule earns
	// its keep under REFNAME_ALLOW_ONELEVEL, which this function has no mode
	// for — and so that adding such a mode does not quietly admit "@".
	if s == "@" {
		return fmt.Errorf("%w: %q", ErrInvalidReferenceName, s)
	}

	// rule 7
	if strings.HasSuffix(s, ".") {
		return fmt.Errorf("%w: %q", ErrInvalidReferenceName, s)
	}

	// rule 2
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return fmt.Errorf("%w: %q", ErrInvalidReferenceName, s)
	}

	for _, part := range parts {
		// rule 6
		if len(part) == 0 {
			return fmt.Errorf("%w: %q", ErrInvalidReferenceName, s)
		}

		if strings.HasPrefix(part, ".") || // rule 1
			strings.Contains(part, "..") || // rule 3
			ctrlSeqs.MatchString(part) || // rule 4
			strings.ContainsAny(part, "~^:?*[ \t\n") || // rule 4 & 5
			strings.Contains(part, "@{") || // rule 8
			strings.Contains(part, "\\") || // rule 10
			strings.HasSuffix(part, ".lock") { // rule 1
			return fmt.Errorf("%w: %q", ErrInvalidReferenceName, s)
		}
	}

	return nil
}

// ValidateBranchName reports whether name, a literal branch shorthand, may be
// used to create a branch. It applies the naming checks from Git's check_branch_ref
// (refs.c): the shorthand must not begin with "-", the resulting reference must
// not be refs/heads/HEAD, and the reference name must satisfy Validate.
// It does not perform repository-dependent expansion of expressions such as
// "@{-1}"; callers must resolve those before validating the resulting name.
//
// The leading "-" is not a check_refname_format rule and Git does not apply it
// to a name arriving over the wire: refs/heads/-foo is a reference git fetch
// and git clone store without complaint, and git update-ref creates on request.
// This restriction applies at creation time. Existing leading-hyphen names can
// still be read, written and deleted; for example, git branch -D -- -foo removes
// an existing branch named -foo.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs.c#L761-L781
func ValidateBranchName(name string) error {
	// Every error names the spliced reference rather than the shorthand, so a
	// caller holding a ReferenceName is told about the name it passed.
	r := NewBranchReferenceName(name)

	// Git compares the shorthand for the "-" rule and the spliced name for the
	// HEAD one; keep that, since the two disagree for a shorthand such as
	// "refs/heads/HEAD", which Git accepts.
	if strings.HasPrefix(name, "-") || r == RefHeadPrefix+"HEAD" {
		return fmt.Errorf("%w: %q", ErrInvalidReferenceName, string(r))
	}

	return r.Validate()
}

// ValidateTagName reports whether name, the shorthand of a tag as a user spells
// it, may be used to create a tag. It mirrors Git's check_tag_ref (refs.c): the
// shorthand must not begin with "-" nor be "HEAD", and the reference name must
// satisfy Validate. See ValidateBranchName for why the "-" rule belongs here
// rather than in Validate.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs.c#L783-L792
func ValidateTagName(name string) error {
	r := NewTagReferenceName(name)

	if strings.HasPrefix(name, "-") || name == "HEAD" {
		return fmt.Errorf("%w: %q", ErrInvalidReferenceName, string(r))
	}

	return r.Validate()
}

const (
	// HEAD is the special reference pointing to the current branch.
	HEAD ReferenceName = "HEAD"
	// Master is the master branch reference.
	Master ReferenceName = "refs/heads/master"
	// Main is the main branch reference.
	Main ReferenceName = "refs/heads/main"
	// Invalid defines an invalid reference target which is used for specific
	// workflows on upstream Git.
	Invalid ReferenceName = "refs/heads/.invalid"
)

// Reference is a representation of git reference
type Reference struct {
	t      ReferenceType
	n      ReferenceName
	h      Hash
	target ReferenceName
}

// NewReferenceFromStrings creates a reference from name and target as string,
// the resulting reference can be a SymbolicReference or a HashReference base
// on the target provided
func NewReferenceFromStrings(name, target string) *Reference {
	n := ReferenceName(name)

	if strings.HasPrefix(target, symrefPrefix) {
		target := ReferenceName(target[len(symrefPrefix):])
		return NewSymbolicReference(n, target)
	}

	return NewHashReference(n, NewHash(target))
}

// NewSymbolicReference creates a new SymbolicReference reference
func NewSymbolicReference(n, target ReferenceName) *Reference {
	return &Reference{
		t:      SymbolicReference,
		n:      n,
		target: target,
	}
}

// NewHashReference creates a new HashReference reference
func NewHashReference(n ReferenceName, h Hash) *Reference {
	return &Reference{
		t: HashReference,
		n: n,
		h: h,
	}
}

// Type returns the type of a reference
func (r *Reference) Type() ReferenceType {
	return r.t
}

// Name returns the name of a reference
func (r *Reference) Name() ReferenceName {
	return r.n
}

// Hash returns the hash of a hash reference
func (r *Reference) Hash() Hash {
	return r.h
}

// Target returns the target of a symbolic reference
func (r *Reference) Target() ReferenceName {
	return r.target
}

// Strings dump a reference as a [2]string
func (r *Reference) Strings() [2]string {
	var o [2]string
	o[0] = r.Name().String()

	switch r.Type() {
	case HashReference:
		o[1] = r.Hash().String()
	case SymbolicReference:
		o[1] = symrefPrefix + r.Target().String()
	}

	return o
}

func (r *Reference) String() string {
	ref := ""
	switch r.Type() {
	case HashReference:
		ref = r.Hash().String()
	case SymbolicReference:
		ref = symrefPrefix + r.Target().String()
	default:
		return ""
	}

	name := r.Name().String()
	var v strings.Builder
	v.Grow(len(ref) + len(name) + 1)
	v.WriteString(ref)
	v.WriteString(" ")
	v.WriteString(name)
	return v.String()
}
