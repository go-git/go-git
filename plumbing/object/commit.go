package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
	"github.com/go-git/go-git/v6/x/plugin"
)

const (
	headerpgp      string = "gpgsig"
	headerpgp256   string = "gpgsig-sha256"
	headerencoding string = "encoding"

	defaultUtf8CommitMessageEncoding MessageEncoding = "UTF-8"
)

// Hash represents the hash of an object
type Hash plumbing.Hash

// MessageEncoding represents the encoding of a commit
type MessageEncoding string

// Commit points to a single tree, marking it as what the project looked like
// at a certain point in time. It contains meta-information about that point
// in time, such as a timestamp, the author of the changes since the last
// commit, a pointer to the previous commit(s), etc.
// http://shafiulazam.com/gitbook/1_the_git_object_model.html
//
// When a Commit is populated by Decode it retains a reference to the source
// plumbing.EncodedObject so that Verify can reproduce the exact bytes its
// signature was computed over. Refer to Verify for more information.
type Commit struct {
	// Hash of the commit object.
	Hash plumbing.Hash
	// Author is the original author of the commit.
	Author Signature
	// Committer is the one performing the commit, might be different from
	// Author.
	Committer Signature
	// Signature is the cryptographic signature of the commit (e.g. SSH, X.509).
	Signature []byte
	// SignatureSHA256 is the SHA-256 cryptographic signature of the commit,
	// stored under the "gpgsig-sha256" header. It may be present alongside
	// Signature on commits produced in hash-algorithm compatibility mode.
	SignatureSHA256 []byte
	// Message is the commit message, contains arbitrary text.
	Message string
	// TreeHash is the hash of the root tree of the commit.
	TreeHash plumbing.Hash
	// ParentHashes are the hashes of the parent commits of the commit.
	ParentHashes []plumbing.Hash
	// Encoding is the encoding of the commit.
	Encoding MessageEncoding
	// List of extra headers of the commit
	ExtraHeaders []ExtraHeader

	s storer.EncodedObjectStorer
	// src holds the encoded object this Commit was decoded from, used by
	// Verify to recover the exact bytes the signature was computed over.
	src plumbing.EncodedObject
	// snap pins the payload-affecting field values captured by Decode, so
	// Verify can prove the exported fields still mirror src without
	// re-decoding it. Only captured for signed commits.
	snap *commitSnapshot
}

// commitSnapshot holds the payload-affecting field values of a Commit as they
// were decoded. Signature and SignatureSHA256 are excluded: they are not part
// of the signed payload, so replacing them must not stop Verify from using
// the stored source bytes.
type commitSnapshot struct {
	hash         plumbing.Hash
	author       Signature
	committer    Signature
	message      string
	treeHash     plumbing.Hash
	encoding     MessageEncoding
	parentHashes []plumbing.Hash
	extraHeaders []ExtraHeader
}

// ExtraHeader holds any non-standard header
type ExtraHeader struct {
	// Header name
	Key string
	// Value of the header
	Value string
}

// Format implements fmt.Formatter for ExtraHeader.
func (h ExtraHeader) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		_, _ = fmt.Fprintf(f, "ExtraHeader{Key: %v, Value: %v}", h.Key, h.Value)
	default:
		_, _ = fmt.Fprintf(f, "%s", h.Key)
		if len(h.Value) > 0 {
			_, _ = fmt.Fprint(f, " ")
			// Content may be spread on multiple lines, if so we need to
			// prepend each of them with a space for "continuation".
			value := strings.TrimSuffix(h.Value, "\n")
			lines := strings.Split(value, "\n")
			_, _ = fmt.Fprint(f, strings.Join(lines, "\n "))
		}
	}
}

// Parse an extra header and indicate whether it may be continue on the next line
func parseExtraHeader(line []byte) (ExtraHeader, bool) {
	split := bytes.SplitN(line, []byte{' '}, 2)

	out := ExtraHeader{
		Key:   string(bytes.TrimRight(split[0], "\n")),
		Value: "",
	}

	if len(split) == 2 {
		out.Value += string(split[1])
		return out, true
	}
	return out, false
}

// GetCommit gets a commit from an object storer and decodes it.
func GetCommit(s storer.EncodedObjectStorer, h plumbing.Hash) (*Commit, error) {
	o, err := s.EncodedObject(plumbing.CommitObject, h)
	if err != nil {
		return nil, err
	}

	return DecodeCommit(s, o)
}

// DecodeCommit decodes an encoded object into a *Commit and associates it to
// the given object storer.
func DecodeCommit(s storer.EncodedObjectStorer, o plumbing.EncodedObject) (*Commit, error) {
	c := &Commit{s: s}
	if err := c.Decode(o); err != nil {
		return nil, err
	}

	return c, nil
}

// Tree returns the Tree from the commit.
func (c *Commit) Tree() (*Tree, error) {
	return GetTree(c.s, c.TreeHash)
}

// PatchContext returns the Patch between the actual commit and the provided one.
// Error will be return if context expires. Provided context must be non-nil.
//
// NOTE: Since version 5.1.0 the renames are correctly handled, the settings
// used are the recommended options DefaultDiffTreeOptions.
func (c *Commit) PatchContext(ctx context.Context, to *Commit) (*Patch, error) {
	fromTree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	var toTree *Tree
	if to != nil {
		toTree, err = to.Tree()
		if err != nil {
			return nil, err
		}
	}

	return fromTree.PatchContext(ctx, toTree)
}

// Patch returns the Patch between the actual commit and the provided one.
//
// NOTE: Since version 5.1.0 the renames are correctly handled, the settings
// used are the recommended options DefaultDiffTreeOptions.
func (c *Commit) Patch(to *Commit) (*Patch, error) {
	return c.PatchContext(context.Background(), to)
}

// Parents return a CommitIter to the parent Commits.
func (c *Commit) Parents() CommitIter {
	return NewCommitIter(c.s,
		storer.NewEncodedObjectLookupIter(c.s, plumbing.CommitObject, c.ParentHashes),
	)
}

// NumParents returns the number of parents in a commit.
func (c *Commit) NumParents() int {
	return len(c.ParentHashes)
}

// ErrParentNotFound is returned when the parent commit is not found.
var ErrParentNotFound = errors.New("commit parent not found")

// ErrMalformedCommit is returned when a commit object cannot be decoded
// because its standard headers (tree, parent, author, committer) are missing,
// duplicated, or out of order.
var ErrMalformedCommit = errors.New("malformed commit")

// Parent returns the ith parent of a commit.
func (c *Commit) Parent(i int) (*Commit, error) {
	if len(c.ParentHashes) == 0 || i > len(c.ParentHashes)-1 {
		return nil, ErrParentNotFound
	}

	return GetCommit(c.s, c.ParentHashes[i])
}

// File returns the file with the specified "path" in the commit and a
// nil error if the file exists. If the file does not exist, it returns
// a nil file and the ErrFileNotFound error.
func (c *Commit) File(path string) (*File, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	return tree.File(path)
}

// Files returns a FileIter allowing to iterate over the Tree
func (c *Commit) Files() (*FileIter, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	return tree.Files(), nil
}

// ID returns the object ID of the commit. The returned value will always match
// the current value of Commit.Hash.
//
// ID is present to fulfill the Object interface.
func (c *Commit) ID() plumbing.Hash {
	return c.Hash
}

// Type returns the type of object. It always returns plumbing.CommitObject.
//
// Type is present to fulfill the Object interface.
func (c *Commit) Type() plumbing.ObjectType {
	return plumbing.CommitObject
}

func (c *Commit) reset() {
	storer := c.s
	*c = Commit{
		Encoding: defaultUtf8CommitMessageEncoding,
		s:        storer,
	}
}

// Decode transforms a plumbing.EncodedObject into a Commit struct.
func (c *Commit) Decode(o plumbing.EncodedObject) (err error) {
	if o.Type() != plumbing.CommitObject {
		return ErrUnsupportedObject
	}

	c.reset()
	c.Hash = o.Hash()
	c.src = o

	reader, err := o.Reader()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(reader, &err)

	r := sync.GetBufioReader(reader)
	defer sync.PutBufioReader(r)

	s := &commitScanner{r: r, c: c}
	for state := scanTree; state != nil; {
		state, err = state(s)
		if err != nil {
			return err
		}
	}
	if !s.sawTree {
		return fmt.Errorf("%w: missing tree header", ErrMalformedCommit)
	}
	c.Message = s.msgbuf.String()

	// Only signed commits can take Verify's stored-bytes path, so the
	// snapshot backing its mutation check is skipped for unsigned ones,
	// keeping their decode allocation-free.
	if len(c.Signature) > 0 || len(c.SignatureSHA256) > 0 {
		c.snap = &commitSnapshot{
			hash:         c.Hash,
			author:       c.Author,
			committer:    c.Committer,
			message:      c.Message,
			treeHash:     c.TreeHash,
			encoding:     c.Encoding,
			parentHashes: slices.Clone(c.ParentHashes),
			extraHeaders: slices.Clone(c.ExtraHeaders),
		}
	}
	return nil
}

// Encode transforms a Commit into a plumbing.EncodedObject.
func (c *Commit) Encode(o plumbing.EncodedObject) error {
	return c.encode(o, true)
}

// EncodeWithoutSignature returns a reader over the canonical encoding of the
// Commit without any signature headers — the payload an object signature is
// computed over when signing.
//
// The payload is always derived from the current struct fields, matching the
// bytes Encode stores: a signature computed over it stays verifiable after
// the commit is encoded and stored. To reproduce the signed payload of an
// object exactly as stored — what verification needs — use SignedPayload;
// Verify does so automatically.
func (c *Commit) EncodeWithoutSignature() (io.Reader, error) {
	return &signedReader{writeTo: func(w io.Writer) error {
		return c.encodeTo(w, false)
	}}, nil
}

// matchesSnapshot reports whether the payload-affecting exported fields still
// equal the values captured when the commit was decoded, meaning c.src holds
// the exact bytes the current field values were read from.
func (c *Commit) matchesSnapshot() bool {
	s := c.snap
	return s != nil &&
		c.Hash == s.hash &&
		c.Author == s.author &&
		c.Committer == s.committer &&
		c.Message == s.message &&
		c.TreeHash == s.treeHash &&
		c.Encoding == s.encoding &&
		slices.Equal(c.ParentHashes, s.parentHashes) &&
		slices.Equal(c.ExtraHeaders, s.extraHeaders)
}

func isStandardHeader(key string) bool {
	switch key {
	case "tree", "parent", "author", "committer",
		headerencoding, headerpgp, headerpgp256:
		return true
	}
	return false
}

func (c *Commit) encode(o plumbing.EncodedObject, includeSig bool) (err error) {
	o.SetType(plumbing.CommitObject)
	w, err := o.Writer()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(w, &err)

	return c.encodeTo(w, includeSig)
}

// encodeTo writes the commit's canonical bytes to w, including the gpgsig and
// gpgsig-sha256 signature headers only when includeSig is true.
func (c *Commit) encodeTo(w io.Writer, includeSig bool) (err error) {
	if _, err = fmt.Fprintf(w, "tree %s\n", c.TreeHash.String()); err != nil {
		return err
	}

	for _, parent := range c.ParentHashes {
		if _, err = fmt.Fprintf(w, "parent %s\n", parent.String()); err != nil {
			return err
		}
	}

	if _, err = fmt.Fprint(w, "author "); err != nil {
		return err
	}

	if err = c.Author.Encode(w); err != nil {
		return err
	}

	if _, err = fmt.Fprint(w, "\ncommitter "); err != nil {
		return err
	}

	if err = c.Committer.Encode(w); err != nil {
		return err
	}

	if string(c.Encoding) != "" && c.Encoding != defaultUtf8CommitMessageEncoding {
		if _, err = fmt.Fprintf(w, "\n%s %s", headerencoding, c.Encoding); err != nil {
			return err
		}
	}

	for _, header := range c.ExtraHeaders {
		if isStandardHeader(header.Key) {
			continue
		}
		if _, err = fmt.Fprintf(w, "\n%s", header); err != nil {
			return err
		}
	}

	if len(c.Signature) > 0 && includeSig {
		if _, err = fmt.Fprint(w, "\n"+headerpgp+" "); err != nil {
			return err
		}

		// Split all the signature lines and re-write with a left padding and
		// newline. Use join for this so it's clear that a newline should not be
		// added after this section, as it will be added when the message is
		// printed.
		if _, err = w.Write(indentSignature(c.Signature)); err != nil {
			return err
		}
	}

	if len(c.SignatureSHA256) > 0 && includeSig {
		if _, err = fmt.Fprint(w, "\n"+headerpgp256+" "); err != nil {
			return err
		}

		if _, err = w.Write(indentSignature(c.SignatureSHA256)); err != nil {
			return err
		}
	}

	if _, err = io.WriteString(w, "\n\n"); err != nil {
		return err
	}
	// Write the message via io.WriteString rather than fmt: fmt copies the
	// whole (potentially large) message into an internal buffer, whereas
	// io.WriteString streams it straight to a StringWriter sink.
	if _, err = io.WriteString(w, c.Message); err != nil {
		return err
	}

	return err
}

// Stats returns the stats of a commit.
func (c *Commit) Stats() (FileStats, error) {
	return c.StatsContext(context.Background())
}

// StatsContext returns the stats of a commit. Error will be return if context
// expires. Provided context must be non-nil.
func (c *Commit) StatsContext(ctx context.Context) (FileStats, error) {
	fromTree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	toTree := &Tree{}
	if c.NumParents() != 0 {
		firstParent, err := c.Parents().Next()
		if err != nil {
			return nil, err
		}

		toTree, err = firstParent.Tree()
		if err != nil {
			return nil, err
		}
	}

	patch, err := toTree.PatchContext(ctx, fromTree)
	if err != nil {
		return nil, err
	}

	return getFileStatsFromFilePatches(patch.FilePatches()), nil
}

func (c *Commit) String() string {
	return fmt.Sprintf(
		"%s %s\nAuthor: %s\nDate:   %s\n\n%s\n",
		plumbing.CommitObject, c.Hash, c.Author.String(),
		c.Author.When.Format(DateFormat), indent(c.Message),
	)
}

// Verify checks the signature of the commit using the Verifier provided via
// opts, or, when none is given, the verifier registered through
// plugin.ObjectVerifier. It returns ErrNotSigned when the commit carries no
// signature.
//
// For a commit populated by Decode whose exported fields have not been
// mutated since, the payload is streamed from the source bytes exactly as
// stored, the returned Verification attests the commit's object id in Object,
// and ErrObjectIntegrity is returned if the source bytes no longer hash to
// that id. For a commit built in memory — or mutated after decoding — the
// payload is the canonical encoding of the current fields, the same bytes
// signing covers, and Object is left zero.
func (c *Commit) Verify(ctx context.Context, opts ...VerifyOption) (*plugin.Verification, error) {
	sig := signatureForFormat(c.Hash.Format(), c.Signature, c.SignatureSHA256)
	if c.matchesSnapshot() {
		v, err := Verify(ctx, attestedPayload(c.src, c.Hash), sig, opts...)
		if err != nil {
			return nil, err
		}
		v.Object = c.Hash
		return v, nil
	}

	payload, err := c.EncodeWithoutSignature()
	if err != nil {
		return nil, err
	}
	return Verify(ctx, payload, sig, opts...)
}

// Less defines a compare function to determine which commit is 'earlier' by:
// - First use Committer.When
// - If Committer.When are equal then use Author.When
// - If Author.When also equal then compare the string value of the hash
func (c *Commit) Less(rhs *Commit) bool {
	return c.Committer.When.Before(rhs.Committer.When) ||
		(c.Committer.When.Equal(rhs.Committer.When) &&
			(c.Author.When.Before(rhs.Author.When) ||
				(c.Author.When.Equal(rhs.Author.When) && c.Hash.Compare(rhs.Hash.Bytes()) < 0)))
}

func indent(t string) string {
	output := make([]string, 0, strings.Count(t, "\n")+1)
	for line := range strings.SplitSeq(t, "\n") {
		if len(line) != 0 {
			line = "    " + line
		}

		output = append(output, line)
	}

	return strings.Join(output, "\n")
}

// CommitIter is a generic closable interface for iterating over commits.
type CommitIter interface {
	Next() (*Commit, error)
	ForEach(func(*Commit) error) error
	Close()
}

// storerCommitIter provides an iterator from commits in an EncodedObjectStorer.
type storerCommitIter struct {
	storer.EncodedObjectIter
	s storer.EncodedObjectStorer
}

// NewCommitIter takes a storer.EncodedObjectStorer and a
// storer.EncodedObjectIter and returns a CommitIter that iterates over all
// commits contained in the storer.EncodedObjectIter.
//
// Any non-commit object returned by the storer.EncodedObjectIter is skipped.
func NewCommitIter(s storer.EncodedObjectStorer, iter storer.EncodedObjectIter) CommitIter {
	return &storerCommitIter{iter, s}
}

// Next moves the iterator to the next commit and returns a pointer to it. If
// there are no more commits, it returns io.EOF.
func (iter *storerCommitIter) Next() (*Commit, error) {
	obj, err := iter.EncodedObjectIter.Next()
	if err != nil {
		return nil, err
	}

	return DecodeCommit(iter.s, obj)
}

// ForEach call the cb function for each commit contained on this iter until
// an error appends or the end of the iter is reached. If ErrStop is sent
// the iteration is stopped but no error is returned. The iterator is closed.
func (iter *storerCommitIter) ForEach(cb func(*Commit) error) error {
	return iter.EncodedObjectIter.ForEach(func(obj plumbing.EncodedObject) error {
		c, err := DecodeCommit(iter.s, obj)
		if err != nil {
			return err
		}

		return cb(c)
	})
}

func (iter *storerCommitIter) Close() {
	iter.EncodedObjectIter.Close()
}
