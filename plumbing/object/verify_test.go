package object

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "unsafe" // for go:linkname

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/x/plugin"
)

//go:linkname resetPluginEntry github.com/go-git/go-git/v6/x/plugin.resetEntry
func resetPluginEntry(name plugin.Name)

const objectVerifierPluginName plugin.Name = "object-verifier"

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

func signedCommit(signature []byte) *Commit {
	return &Commit{
		Author:    Signature{Name: "go-git", Email: "go-git@example.com"},
		Committer: Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:   "a message\n",
		TreeHash:  plumbing.NewHash("52a266a58f2c028ad7de4dfd3a72fdf76b0d4e24"),
		Signature: signature,
	}
}

func TestCommitVerifyWithVerifier(t *testing.T) {
	t.Parallel()

	want := &plugin.Verification{Signer: "fp", Method: plugin.SignatureTypeOpenPGP}
	fv := &fakeVerifier{result: want}

	got, err := signedCommit(testSignature).Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Same(t, want, got)

	assert.Equal(t, testSignature, fv.gotSignature, "verifier must receive the embedded signature")
	assert.NotContains(t, string(fv.gotMessage), "gpgsig",
		"verifier must receive the signature-stripped payload")
}

func TestTagVerifyWithVerifier(t *testing.T) {
	t.Parallel()

	tag := &Tag{
		Name:       "v1",
		Tagger:     Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    "a tag\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature:  testSignature,
	}
	want := &plugin.Verification{Signer: "fp", Method: plugin.SignatureTypeOpenPGP}
	fv := &fakeVerifier{result: want}

	got, err := tag.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Same(t, want, got)
	assert.Equal(t, testSignature, fv.gotSignature)
	assert.NotContains(t, string(fv.gotMessage), "BEGIN PGP SIGNATURE",
		"tag payload must have the trailing signature truncated")
}

func TestVerifyUnsigned(t *testing.T) {
	t.Parallel()

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := signedCommit(nil).Verify(context.Background(), WithVerifier(fv))
	assert.ErrorIs(t, err, ErrNotSigned)
	assert.Nil(t, fv.gotSignature, "verifier must not be called for unsigned objects")
}

func TestVerifyUsesRegisteredObjectVerifier(t *testing.T) { //nolint:paralleltest // modifies global plugin state
	resetPluginEntry(objectVerifierPluginName)

	want := &plugin.Verification{Signer: "fp"}
	fv := &fakeVerifier{result: want}
	require.NoError(t, plugin.Register(plugin.ObjectVerifier(), func() plugin.Verifier { return fv }))

	got, err := signedCommit(testSignature).Verify(context.Background())
	require.NoError(t, err)
	assert.Same(t, want, got)
	assert.Equal(t, testSignature, fv.gotSignature)
}

func TestVerifyWithoutVerifierOrPlugin(t *testing.T) { //nolint:paralleltest // modifies global plugin state
	resetPluginEntry(objectVerifierPluginName)

	_, err := signedCommit(testSignature).Verify(context.Background())
	assert.True(t, errors.Is(err, plugin.ErrNotFound))
}

func TestVerifyDoesNotFreezeUnregisteredVerifier(t *testing.T) { //nolint:paralleltest // modifies global plugin state
	resetPluginEntry(objectVerifierPluginName)

	// A Verify with nothing registered must not freeze the plugin entry, so a
	// later Register still succeeds (regression: plugin.Get would freeze it).
	_, err := signedCommit(testSignature).Verify(context.Background())
	require.ErrorIs(t, err, plugin.ErrNotFound)

	fv := &fakeVerifier{result: &plugin.Verification{}}
	require.NoError(t, plugin.Register(plugin.ObjectVerifier(), func() plugin.Verifier { return fv }))

	_, err = signedCommit(testSignature).Verify(context.Background())
	require.NoError(t, err)
}

func TestVerifyMutatedCommitReflectsMutation(t *testing.T) {
	t.Parallel()

	// Encode a signed commit and decode it so its source is set.
	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedCommit(testSignature).Encode(enc))
	decoded := &Commit{}
	require.NoError(t, decoded.Decode(enc))

	// Mutating an exported field after decode must be reflected in what verify
	// checks: a mutable Commit is verified over its current field values, so a
	// tampered message changes the signed payload the verifier receives.
	// (Source-pinned, immutable verification is the job of the read-only views
	// in x/plumbing/object, built on SignedPayload, not of Commit.)
	decoded.Message = "tampered\n"

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Contains(t, string(fv.gotMessage), "tampered",
		"verify must reproduce the mutated field, not the stored source bytes")
	assert.NotContains(t, string(fv.gotMessage), "a message")
	assert.NotContains(t, string(fv.gotMessage), "gpgsig")
}

func TestSignedPayloadCommit(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedCommit(testSignature).Encode(enc))

	r, err := SignedPayload(enc)
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)

	assert.Contains(t, string(got), "a message")
	assert.NotContains(t, string(got), "gpgsig",
		"commit signature headers must be stripped")
}

func TestSignedPayloadTag(t *testing.T) {
	t.Parallel()

	tag := &Tag{
		Name:       "v1",
		Tagger:     Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    "a tag\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature:  testSignature,
	}
	enc := &plumbing.MemoryObject{}
	require.NoError(t, tag.Encode(enc))

	r, err := SignedPayload(enc)
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)

	assert.Contains(t, string(got), "a tag")
	assert.NotContains(t, string(got), "BEGIN PGP SIGNATURE",
		"tag trailing signature must be truncated")
}

func TestSignedPayloadUnsupported(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	enc.SetType(plumbing.BlobObject)

	_, err := SignedPayload(enc)
	assert.ErrorIs(t, err, ErrUnsupportedObject)
}

func TestVerifyFunc(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedCommit(testSignature).Encode(enc))
	payload, err := SignedPayload(enc)
	require.NoError(t, err)

	want := &plugin.Verification{Signer: "fp"}
	fv := &fakeVerifier{result: want}
	got, err := Verify(context.Background(), payload, testSignature, WithVerifier(fv))
	require.NoError(t, err)
	assert.Same(t, want, got)
	assert.Equal(t, testSignature, fv.gotSignature)
	assert.NotContains(t, string(fv.gotMessage), "gpgsig")
}

func TestVerifyFuncUnsigned(t *testing.T) {
	t.Parallel()

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := Verify(context.Background(), strings.NewReader("payload"), nil, WithVerifier(fv))
	assert.ErrorIs(t, err, ErrNotSigned)
	assert.Nil(t, fv.gotSignature, "verifier must not be called when signature is empty")
}

func TestVerifyDecodedTag(t *testing.T) {
	t.Parallel()

	tag := &Tag{
		Name:       "v1",
		Tagger:     Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    "a tag\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature:  testSignature,
	}
	enc := &plumbing.MemoryObject{}
	require.NoError(t, tag.Encode(enc))
	decoded := &Tag{}
	require.NoError(t, decoded.Decode(enc))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignature, fv.gotSignature)
	assert.Contains(t, string(fv.gotMessage), "a tag")
	assert.NotContains(t, string(fv.gotMessage), "BEGIN PGP SIGNATURE",
		"tag payload must have the trailing signature truncated")
}
