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
	"github.com/go-git/go-git/v6/plumbing/format/config"
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
	// checks: the payload falls back to the canonical encoding of the current
	// fields — the same bytes signing covers — so a tampered message can never
	// verify against the signature of the stored object.
	decoded.Message = "tampered\n"

	fv := &fakeVerifier{result: &plugin.Verification{}}
	got, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Contains(t, string(fv.gotMessage), "tampered",
		"verify must reproduce the mutated field, not the stored source bytes")
	assert.NotContains(t, string(fv.gotMessage), "a message")
	assert.NotContains(t, string(fv.gotMessage), "gpgsig")
	assert.True(t, got.Object.IsZero(),
		"a payload re-encoded from mutated fields must not attest a stored object id")
}

var testSignatureSHA256 = []byte("-----BEGIN PGP SIGNATURE-----\n\nsha256sig\n-----END PGP SIGNATURE-----\n")

func TestSignatureForFormat(t *testing.T) {
	t.Parallel()

	assert.Equal(t, testSignature, signatureForFormat(config.SHA1, testSignature, testSignatureSHA256),
		"sha1 object must select the default signature")
	assert.Equal(t, testSignatureSHA256, signatureForFormat(config.SHA256, testSignature, testSignatureSHA256),
		"sha256 object must select the gpgsig-sha256 signature")
}

func TestCommitVerifyUsesSHA256SignatureForSHA256Object(t *testing.T) {
	t.Parallel()

	c := signedCommit(testSignature)
	c.SignatureSHA256 = testSignatureSHA256

	enc := plumbing.NewMemoryObject(plumbing.FromObjectFormat(config.SHA256))
	require.NoError(t, c.Encode(enc))
	require.Equal(t, config.SHA256Size, enc.Hash().Size(), "object must be sha256")

	decoded := &Commit{}
	require.NoError(t, decoded.Decode(enc))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignatureSHA256, fv.gotSignature,
		"a sha256 commit must be verified with its gpgsig-sha256 signature")
}

func TestTagVerifyNativeSHA256UsesInlineSignature(t *testing.T) {
	t.Parallel()

	// A tag signed in a native sha256 repository carries only the inline
	// trailing signature, exactly like a sha1 tag (builtin/tag.c:do_sign).
	enc := plumbing.NewMemoryObject(plumbing.FromObjectFormat(config.SHA256))
	require.NoError(t, signedTag(testSignature).Encode(enc))
	require.Equal(t, config.SHA256Size, enc.Hash().Size(), "object must be sha256")

	decoded := &Tag{}
	require.NoError(t, decoded.Decode(enc))
	require.Empty(t, decoded.SignatureSHA256)

	fv := &fakeVerifier{result: &plugin.Verification{}}
	got, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignature, fv.gotSignature,
		"a native sha256 tag must be verified with its inline signature")
	assert.True(t, got.Object.Equal(enc.Hash()))
}

func TestTagVerifyPrefersInlineSignatureOverSHA256Header(t *testing.T) {
	t.Parallel()

	// A tag's own-payload signature is always the inline one; gpgsig-sha256
	// headers on tags carry the signature of the other rendition in hash
	// compatibility mode (gpg-interface.c:parse_signature verifies only the
	// inline block).
	tag := signedTag(testSignature)
	tag.SignatureSHA256 = testSignatureSHA256

	enc := plumbing.NewMemoryObject(plumbing.FromObjectFormat(config.SHA256))
	require.NoError(t, tag.Encode(enc))

	decoded := &Tag{}
	require.NoError(t, decoded.Decode(enc))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, testSignature, fv.gotSignature,
		"the inline signature covers the tag's own payload; headers cover other renditions")
}

func TestTagVerifyHeaderOnlySignatureIsNotSigned(t *testing.T) {
	t.Parallel()

	// Upstream verification only parses the inline block; a tag carrying only
	// a gpgsig-sha256 header reports "no signature found".
	tag := signedTag(nil)
	tag.SignatureSHA256 = testSignatureSHA256

	enc := plumbing.NewMemoryObject(plumbing.FromObjectFormat(config.SHA256))
	require.NoError(t, tag.Encode(enc))

	decoded := &Tag{}
	require.NoError(t, decoded.Decode(enc))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err := decoded.Verify(context.Background(), WithVerifier(fv))
	assert.ErrorIs(t, err, ErrNotSigned)
	assert.Nil(t, fv.gotSignature)
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

func signedTag(signature []byte) *Tag {
	return &Tag{
		Name:       "v1",
		Tagger:     Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    "a tag\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature:  signature,
	}
}

func TestCommitVerifyNilVerification(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedCommit(testSignature).Encode(enc))
	decoded := &Commit{}
	require.NoError(t, decoded.Decode(enc))

	// A broken verifier returning (nil, nil) must surface as an error, not a
	// panic when the stored-bytes path attests the object id.
	fv := &fakeVerifier{}
	_, err := decoded.Verify(context.Background(), WithVerifier(fv))
	assert.ErrorIs(t, err, ErrNilVerification)
}

func TestTagVerifyNilVerification(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedTag(testSignature).Encode(enc))
	decoded := &Tag{}
	require.NoError(t, decoded.Decode(enc))

	fv := &fakeVerifier{}
	_, err := decoded.Verify(context.Background(), WithVerifier(fv))
	assert.ErrorIs(t, err, ErrNilVerification)
}

func TestCommitVerifyAttestsStoredObject(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedCommit(testSignature).Encode(enc))
	decoded := &Commit{}
	require.NoError(t, decoded.Decode(enc))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	got, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.True(t, got.Object.Equal(enc.Hash()),
		"a verification over stored bytes must attest the object id")
}

func TestTagVerifyAttestsStoredObject(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedTag(testSignature).Encode(enc))
	decoded := &Tag{}
	require.NoError(t, decoded.Decode(enc))

	fv := &fakeVerifier{result: &plugin.Verification{}}
	got, err := decoded.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.True(t, got.Object.Equal(enc.Hash()),
		"a verification over stored bytes must attest the object id")
}

func TestCommitVerifySourceIntegrity(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedCommit(testSignature).Encode(enc))
	decoded := &Commit{}
	require.NoError(t, decoded.Decode(enc))

	// MemoryObject caches its hash on first use, so appending afterwards
	// yields a source whose bytes no longer hash to the object id — standing
	// in for a corrupt or substituted object returned by a store.
	_, err := enc.Write([]byte("trailing garbage"))
	require.NoError(t, err)

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err = decoded.Verify(context.Background(), WithVerifier(fv))
	assert.ErrorIs(t, err, ErrObjectIntegrity,
		"source bytes that do not hash to the object id must not verify")
}

func TestTagVerifySourceIntegrity(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedTag(testSignature).Encode(enc))
	decoded := &Tag{}
	require.NoError(t, decoded.Decode(enc))

	_, err := enc.Write([]byte("trailing garbage"))
	require.NoError(t, err)

	fv := &fakeVerifier{result: &plugin.Verification{}}
	_, err = decoded.Verify(context.Background(), WithVerifier(fv))
	assert.ErrorIs(t, err, ErrObjectIntegrity,
		"source bytes that do not hash to the object id must not verify")
}

func TestSignVerifyRoundtripNonCanonicalSource(t *testing.T) {
	t.Parallel()

	// A stored commit whose bytes are not byte-identical to go-git's
	// canonical encoding: the explicit "encoding UTF-8" header is dropped on
	// re-encode. A signature computed over the stored bytes could never be
	// verified once the commit is re-encoded and stored, so signing must
	// cover the canonical bytes instead.
	raw := "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n" +
		"author John Doe <john.doe@example.com> 1755280730 -0700\n" +
		"committer John Doe <john.doe@example.com> 1755280730 -0700\n" +
		"encoding UTF-8\n" +
		"\n" +
		"initial commit\n"
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.CommitObject)
	_, err := obj.Write([]byte(raw))
	require.NoError(t, err)

	c := &Commit{}
	require.NoError(t, c.Decode(obj))

	r, err := c.EncodeWithoutSignature()
	require.NoError(t, err)
	signed, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.NotContains(t, string(signed), "encoding UTF-8",
		"the signing payload must be the canonical bytes Encode will store")

	c.Signature = testSignature
	stored := &plumbing.MemoryObject{}
	require.NoError(t, c.Encode(stored))

	verified := &Commit{}
	require.NoError(t, verified.Decode(stored))
	fv := &fakeVerifier{result: &plugin.Verification{}}
	got, err := verified.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, string(signed), string(fv.gotMessage),
		"the verified payload must be byte-identical to the signed payload")
	assert.True(t, got.Object.Equal(stored.Hash()))
}

func TestSignVerifyRoundtripAfterMutation(t *testing.T) {
	t.Parallel()

	enc := &plumbing.MemoryObject{}
	require.NoError(t, signedCommit(testSignature).Encode(enc))
	c := &Commit{}
	require.NoError(t, c.Decode(enc))

	// Amend the decoded commit and re-sign it, as a caller rewriting an
	// existing object would.
	c.Message = "amended message\n"
	r, err := c.EncodeWithoutSignature()
	require.NoError(t, err)
	signed, err := io.ReadAll(r)
	require.NoError(t, err)
	c.Signature = []byte("a new signature")

	stored := &plumbing.MemoryObject{}
	require.NoError(t, c.Encode(stored))

	verified := &Commit{}
	require.NoError(t, verified.Decode(stored))
	fv := &fakeVerifier{result: &plugin.Verification{}}
	got, err := verified.Verify(context.Background(), WithVerifier(fv))
	require.NoError(t, err)
	assert.Equal(t, string(signed), string(fv.gotMessage),
		"the verified payload must be byte-identical to the re-signed payload")
	assert.True(t, got.Object.Equal(stored.Hash()))
}
