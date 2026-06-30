package object_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	gitobject "github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/x/plugin"
	"github.com/go-git/go-git/v6/x/plumbing/object"
)

var testSignature = []byte("-----BEGIN PGP SIGNATURE-----\n\nabc\n-----END PGP SIGNATURE-----\n")

type fakeVerifier struct {
	gotMessage   []byte
	gotSignature []byte
	result       *plugin.Verification
	err          error
}

func (f *fakeVerifier) Verify(_ context.Context, message io.Reader, signature []byte) (*plugin.Verification, error) {
	b, err := io.ReadAll(message)
	if err != nil {
		return nil, err
	}
	f.gotMessage = b
	f.gotSignature = signature
	return f.result, f.err
}

func signedCommit(signature []byte) *gitobject.Commit {
	return &gitobject.Commit{
		Author:    gitobject.Signature{Name: "go-git", Email: "go-git@example.com"},
		Committer: gitobject.Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:   "a message\n",
		TreeHash:  plumbing.NewHash("52a266a58f2c028ad7de4dfd3a72fdf76b0d4e24"),
		Signature: signature,
	}
}

func signedTag(signature []byte) *gitobject.Tag {
	return &gitobject.Tag{
		Name:       "v1",
		Tagger:     gitobject.Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    "a tag\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature:  signature,
	}
}

func decodeReadCommit(t *testing.T, c *gitobject.Commit) *object.ReadCommit {
	t.Helper()
	enc := &plumbing.MemoryObject{}
	require.NoError(t, c.Encode(enc))
	rc, err := object.DecodeReadCommit(nil, enc)
	require.NoError(t, err)
	return rc
}

func decodeReadTag(t *testing.T, tag *gitobject.Tag) *object.ReadTag {
	t.Helper()
	enc := &plumbing.MemoryObject{}
	require.NoError(t, tag.Encode(enc))
	rt, err := object.DecodeReadTag(nil, enc)
	require.NoError(t, err)
	return rt
}

func TestReadCommitVerify(t *testing.T) {
	t.Parallel()

	rc := decodeReadCommit(t, signedCommit(testSignature))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := rc.Verify(context.Background(), gitobject.WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignature, fv.gotSignature)
	assert.Contains(t, string(fv.gotMessage), "a message")
	assert.NotContains(t, string(fv.gotMessage), "gpgsig",
		"verifier must receive the signature-stripped payload")
}

func TestReadCommitGetters(t *testing.T) {
	t.Parallel()

	c := signedCommit(testSignature)
	c.ParentHashes = []plumbing.Hash{
		plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
	}
	rc := decodeReadCommit(t, c)

	assert.Equal(t, c.Author.Name, rc.Author().Name)
	assert.Equal(t, c.Committer.Name, rc.Committer().Name)
	assert.Equal(t, "a message\n", rc.Message())
	assert.Equal(t, c.TreeHash, rc.TreeHash())
	assert.Equal(t, c.ParentHashes, rc.ParentHashes())
	assert.Equal(t, testSignature, rc.Signature())
}

func TestReadCommitGettersReturnCopies(t *testing.T) {
	t.Parallel()

	c := signedCommit(testSignature)
	c.ParentHashes = []plumbing.Hash{
		plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
	}
	rc := decodeReadCommit(t, c)

	sig := rc.Signature()
	sig[0] = 'X'
	parents := rc.ParentHashes()
	parents[0] = plumbing.ZeroHash

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := rc.Verify(context.Background(), gitobject.WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignature, fv.gotSignature,
		"mutating a returned slice must not change the verified signature")
	assert.Equal(t, testSignature, rc.Signature())
	assert.NotEqual(t, plumbing.ZeroHash, rc.ParentHashes()[0])
}

func TestReadCommitVerifyUnsigned(t *testing.T) {
	t.Parallel()

	rc := decodeReadCommit(t, signedCommit(nil))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := rc.Verify(context.Background(), gitobject.WithVerifier(fv))
	require.ErrorIs(t, err, gitobject.ErrNotSigned)
	assert.Nil(t, fv.gotSignature)
}

func TestReadTagVerify(t *testing.T) {
	t.Parallel()

	rt := decodeReadTag(t, signedTag(testSignature))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := rt.Verify(context.Background(), gitobject.WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignature, fv.gotSignature)
	assert.Contains(t, string(fv.gotMessage), "a tag")
	assert.NotContains(t, string(fv.gotMessage), "BEGIN PGP SIGNATURE",
		"tag payload must have the trailing signature truncated")
}

func TestReadTagGetters(t *testing.T) {
	t.Parallel()

	tag := signedTag(testSignature)
	rt := decodeReadTag(t, tag)

	assert.Equal(t, "v1", rt.Name())
	assert.Equal(t, tag.Tagger.Name, rt.Tagger().Name)
	assert.Equal(t, "a tag\n", rt.Message())
	assert.Equal(t, plumbing.CommitObject, rt.TargetType())
	assert.Equal(t, tag.Target, rt.Target())
	assert.Equal(t, testSignature, rt.Signature())
}

func TestReadTagGettersReturnCopies(t *testing.T) {
	t.Parallel()

	rt := decodeReadTag(t, signedTag(testSignature))

	sig := rt.Signature()
	sig[0] = 'X'

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := rt.Verify(context.Background(), gitobject.WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignature, fv.gotSignature)
	assert.Equal(t, testSignature, rt.Signature())
}
