package commitgraph

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestMalformedSignatureTraversal(t *testing.T) {
	t.Parallel()
	for _, signature := range []string{"Bad <bad>", "Bad <bad> nonsense +0000", "Bad <bad> 9223372036854775808 +0000", "Bad <bad> -9223372036854775809 +0000", ">01", "Bad bad> 1 +0000", "Bad >bad< 1 +0000"} {
		t.Run(signature, func(t *testing.T) {
			t.Parallel()
			obj := encodedCommit(t, "tree "+treeHex+"\nauthor "+signature+"\ncommitter "+signature+"\n\nmessage\n")
			s := memory.NewStorage()
			id, err := s.SetEncodedObject(obj)
			require.NoError(t, err)
			_, err = object.GetCommit(s, id)
			require.NoError(t, err)
			for _, index := range []CommitNodeIndex{NewObjectCommitNodeIndex(s), NewGraphCommitNodeIndex(nil, s)} {
				node, err := index.Get(id)
				require.NoError(t, err)
				require.Equal(t, time.Unix(0, 0).UTC(), node.CommitTime())
				authorWhen, err := nodeAuthorTime(node)
				require.NoError(t, err)
				require.Equal(t, time.Unix(0, 0).UTC(), authorWhen)
			}
		})
	}
}

func TestTraversalRejectsWrongHashWidths(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		"tree " + strings.Repeat("a", 64) + "\n",
		"tree " + treeHex + "\nparent " + strings.Repeat("b", 64) + "\n",
	} {
		s := memory.NewStorage()
		id, err := s.SetEncodedObject(encodedCommit(t, content))
		require.NoError(t, err)
		_, err = NewObjectCommitNodeIndex(s).Get(id)
		require.ErrorIs(t, err, object.ErrMalformedCommit)
	}
}

func TestParseIntBytesBounds(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"9223372036854775808", "-9223372036854775809", "18446744073709551616"} {
		_, ok := parseIntBytes([]byte(in))
		require.False(t, ok, in)
	}
	for _, tc := range []struct {
		in   string
		want int64
	}{{"9223372036854775807", 9223372036854775807}, {"-9223372036854775808", -9223372036854775808}} {
		got, ok := parseIntBytes([]byte(tc.in))
		require.True(t, ok)
		require.Equal(t, tc.want, got)
	}
}

func TestFullCommitCompatibility(t *testing.T) {
	t.Parallel()
	content := "tree " + treeHex + "\nauthor Alice <a> 1 +0000\ncommitter Bob <b> 2 +0000\ngpgsig signature\n continuation\n\nmessage\n"
	s := memory.NewStorage()
	id, err := s.SetEncodedObject(encodedCommit(t, content))
	require.NoError(t, err)
	for _, index := range []CommitNodeIndex{NewObjectCommitNodeIndex(s), NewGraphCommitNodeIndex(nil, s)} {
		node, err := index.Get(id)
		require.NoError(t, err)
		var full *object.Commit
		full, err = node.Commit()
		require.NoError(t, err)
		require.Equal(t, "message\n", full.Message)
		require.Equal(t, "Alice", full.Author.Name)
		require.Equal(t, "signature\ncontinuation\n", full.Signature)
	}
}

type suppliedHashObject struct{ plumbing.EncodedObject }

func (o suppliedHashObject) Hash() plumbing.Hash { return plumbing.ZeroHash }

type suppliedHashStore struct{ *memory.Storage }

func (s suppliedHashStore) EncodedObject(typ plumbing.ObjectType, id plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := s.Storage.EncodedObject(typ, id)
	if err != nil {
		return nil, err
	}
	return suppliedHashObject{obj}, nil
}

func TestTraversalUsesLookupIdentity(t *testing.T) {
	t.Parallel()
	s := memory.NewStorage()
	id, err := s.SetEncodedObject(encodedCommit(t, "tree "+treeHex+"\nauthor Bad <bad>\ncommitter Good <good> 123 +0100\n"))
	require.NoError(t, err)
	for _, index := range []CommitNodeIndex{NewObjectCommitNodeIndex(suppliedHashStore{s}), NewGraphCommitNodeIndex(nil, suppliedHashStore{s})} {
		node, err := index.Get(id)
		require.NoError(t, err)
		require.Equal(t, id, node.ID())
		require.Equal(t, int64(123), node.CommitTime().Unix())
	}
}

type countingStore struct {
	*memory.Storage
	reads int
}

func (s *countingStore) EncodedObject(typ plumbing.ObjectType, id plumbing.Hash) (plumbing.EncodedObject, error) {
	s.reads++
	return s.Storage.EncodedObject(typ, id)
}

func TestGraphNodeCachesAuthorTime(t *testing.T) {
	t.Parallel()
	s := &countingStore{Storage: memory.NewStorage()}
	id, err := s.SetEncodedObject(encodedCommit(t, "tree "+treeHex+"\nauthor A <a> 42 +0000\ncommitter B <b> 43 +0000\n"))
	require.NoError(t, err)
	node := &graphCommitNode{hash: id, gci: &graphCommitNodeIndex{s: s}}
	for range 3 {
		got, err := node.authorTime()
		require.NoError(t, err)
		require.Equal(t, int64(42), got.Unix())
	}
	require.Equal(t, 1, s.reads)
}

type customAuthorNode struct {
	CommitNode
	calls int
}

func (n *customAuthorNode) Commit() (*object.Commit, error) {
	n.calls++
	return &object.Commit{Author: object.Signature{When: time.Unix(42, 0).UTC()}}, nil
}

func TestCustomNodeAuthorTimeCompatibility(t *testing.T) {
	t.Parallel()
	n := &customAuthorNode{}
	got, err := nodeAuthorTime(n)
	require.NoError(t, err)
	require.Equal(t, time.Unix(42, 0).UTC(), got)
	require.Equal(t, 1, n.calls)
}

func TestDecodeTraversalFormatAndEOF(t *testing.T) {
	t.Parallel()
	for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
		t.Run(string(of), func(t *testing.T) {
			t.Parallel()
			tree := strings.Repeat("a", of.Size()*2)
			var id plumbing.Hash
			id.ResetBySize(of.Size())
			for _, last := range []string{"author A <a> 123 +0000", "committer B <b> 456 +0000"} {
				c, err := decodeTraversalCommit(encodedCommit(t, "tree "+tree+"\n"+last), id)
				require.NoError(t, err)
				require.Equal(t, of.Size(), c.Tree().Size())
				if strings.HasPrefix(last, "author") {
					require.Equal(t, int64(123), c.AuthorWhen().Unix())
				} else {
					require.Equal(t, int64(456), c.When().Unix())
				}
			}
			other := formatcfg.SHA1
			if of == other {
				other = formatcfg.SHA256
			}
			_, err := decodeTraversalCommit(encodedCommit(t, "tree "+strings.Repeat("a", other.Size()*2)+"\n"), id)
			require.ErrorIs(t, err, object.ErrMalformedCommit)
		})
	}
}

type faultyReader struct {
	io.Reader
	readErr, closeErr error
	closes            int
}

func (r *faultyReader) Read(p []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	return r.Reader.Read(p)
}

func (r *faultyReader) Close() error {
	r.closes++
	return r.closeErr
}

type suppliedReaderObject struct {
	plumbing.EncodedObject
	reader *faultyReader
	err    error
}

func (o suppliedReaderObject) Reader() (io.ReadCloser, error) { return o.reader, o.err }

func TestTraversalReaderErrors(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"open", "read", "close"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			sentinel := errors.New(mode)
			r := &faultyReader{Reader: strings.NewReader("tree " + treeHex + "\n")}
			o := suppliedReaderObject{EncodedObject: encodedCommit(t, ""), reader: r}
			switch mode {
			case "open":
				o.err = sentinel
			case "read":
				r.readErr = sentinel
			case "close":
				r.closeErr = sentinel
			}
			_, err := decodeTraversalCommit(o, plumbing.ZeroHash)
			require.ErrorIs(t, err, sentinel)
			want := 1
			if mode == "open" {
				want = 0
			}
			require.Equal(t, want, r.closes)
		})
	}
}

func TestTraversalMatchesFullCommitFields(t *testing.T) {
	t.Parallel()
	content := "tree " + treeHex + "\nparent " + parent1Hex + "\nauthor Alice <a> 100 -0530\ncommitter Bob <b> 200 +0130\n\nmessage\n"
	obj := encodedCommit(t, content)
	full, err := object.DecodeCommit(memory.NewStorage(), obj)
	require.NoError(t, err)
	light, err := decodeTestCommit(obj)
	require.NoError(t, err)
	require.Equal(t, full.Hash, light.ID())
	require.Equal(t, full.TreeHash, light.Tree())
	require.Equal(t, full.ParentHashes, light.Parents())
	require.Equal(t, full.Author.When, light.AuthorWhen())
	require.Equal(t, full.Committer.When, light.When())
}

type customWalkNode struct {
	CommitNode
	calls *int
}

func (n customWalkNode) Commit() (*object.Commit, error) {
	*n.calls++
	return n.CommitNode.Commit()
}

func (n customWalkNode) ParentNode(i int) (CommitNode, error) {
	node, err := n.CommitNode.ParentNode(i)
	if err != nil {
		return nil, err
	}
	return customWalkNode{node, n.calls}, nil
}

func (n customWalkNode) ParentNodes() CommitNodeIter {
	return newParentgraphCommitNodeIter(n)
}

func TestCustomNodeAuthorOrderWalk(t *testing.T) {
	t.Parallel()
	s := memory.NewStorage()
	parents := make([]plumbing.Hash, 0, 2)
	for _, timestamp := range []string{"1", "2"} {
		id, err := s.SetEncodedObject(encodedCommit(t, "tree "+treeHex+"\nauthor A <a> "+timestamp+" +0000\ncommitter B <b> 3 +0000\n\nroot\n"))
		require.NoError(t, err)
		parents = append(parents, id)
	}
	content := "tree " + treeHex + "\nparent " + parents[0].String() + "\nparent " + parents[1].String() + "\nauthor A <a> 4 +0000\ncommitter B <b> 4 +0000\n\nmerge\n"
	id, err := s.SetEncodedObject(encodedCommit(t, content))
	require.NoError(t, err)
	root, err := NewObjectCommitNodeIndex(s).Get(id)
	require.NoError(t, err)
	calls := 0
	var visited []plumbing.Hash
	err = NewCommitNodeIterAuthorDateOrder(customWalkNode{root, &calls}, nil, nil).ForEach(func(node CommitNode) error {
		visited = append(visited, node.ID())
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []plumbing.Hash{id, parents[1], parents[0]}, visited)
	require.Positive(t, calls)
}

func TestSHA256LookupIdentityWithoutEncodedHash(t *testing.T) {
	t.Parallel()
	id := plumbing.NewHash(strings.Repeat("d", 64))
	obj := suppliedHashObject{encodedCommit(t, "tree "+strings.Repeat("a", 64)+"\nauthor A <a> 1 +0000\ncommitter B <b> 2 +0000\n")}
	c, err := decodeTraversalCommit(obj, id)
	require.NoError(t, err)
	require.Equal(t, id, c.ID())
	require.Equal(t, 32, c.Tree().Size())
}

func TestNegativeZeroHourOffset(t *testing.T) {
	t.Parallel()
	// Git keeps the sign of a -00MM offset; object.Signature drops it. Both
	// decode the same instant, so only the zone differs.
	c, err := decodeTestCommit(encodedCommit(t, "tree "+treeHex+"\nauthor A <a> 1000 -0030\ncommitter C <c> 1000 -0030\n"))
	require.NoError(t, err)
	for _, when := range []time.Time{c.AuthorWhen(), c.When()} {
		_, offset := when.Zone()
		require.Equal(t, -30*60, offset)
		require.Equal(t, int64(1000), when.Unix())
	}
}
