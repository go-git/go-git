package transport

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// shallowAnswer is what a server made of a v2 fetch with a shallow request:
// the lines of its shallow-info section with commit names for ids, sorted,
// or that it refused the request.
type shallowAnswer struct {
	lines   []string
	refused bool
}

// v2ShallowAnswers sends the same v2 fetch to git and to go-git.
func v2ShallowAnswers(t *testing.T, r *canonicalRepo, args ...string) (canonical, got shallowAnswer) {
	t.Helper()
	// Versions before protocol v2 parse the command as a v0 want line.
	requireGitAtLeast(t, 2, 18, "protocol v2")

	req, err := io.ReadAll(v2Request(t, "fetch", nil, append(args, "done")))
	require.NoError(t, err)

	parse := func(out []byte, failed bool) shallowAnswer {
		a := shallowAnswer{refused: failed}
		for _, line := range pktLines(t, out) {
			kind, id, ok := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
			if !ok || (kind != "shallow" && kind != "unshallow") {
				continue
			}
			names := r.names([]plumbing.Hash{plumbing.NewHash(id)})
			a.lines = append(a.lines, kind+" "+names[0])
		}
		slices.Sort(a.lines)
		return a
	}

	cmd := gitenv.Command("git", "upload-pack", "--stateless-rpc", r.dir)
	cmd.Env = append(cmd.Env, "GIT_PROTOCOL=version=2")
	cmd.Stdin = bytes.NewReader(req)
	out, err := cmd.Output()
	canonical = parse(out, err != nil)

	var buf bytes.Buffer
	err = UploadPack(context.Background(), r.st, io.NopCloser(bytes.NewReader(req)),
		ioutil.WriteNopCloser(&buf), &UploadPackRequest{GitProtocol: "version=2", StatelessRPC: true})
	got = parse(buf.Bytes(), err != nil)
	return canonical, got
}

// selectionHistory is c1 <- c2 <- c3 <- c4 <- c5 with s1 on c3 and the merge
// m of c5 and s1, dated a thousand seconds apart from 1700001000 (c1) to
// 1700005000 (c5), s1 at 1700004500 and m at 1700006000.
func selectionHistory(t *testing.T) *canonicalRepo {
	t.Helper()
	r := newCanonicalRepo(t)
	at := func(seconds int64, name string, parents ...string) {
		r.when = time.Unix(1700000000+seconds, 0).UTC()
		r.commit(name, parents...)
	}
	at(1000, "c1")
	at(2000, "c2", "c1")
	at(3000, "c3", "c2")
	at(4000, "c4", "c3")
	at(5000, "c5", "c4")
	at(4500, "s1", "c3")
	at(6000, "m", "c5", "s1")
	r.branch("main", "m")
	r.branch("old", "c2")
	require.NoError(t, r.st.SetReference(plumbing.NewHashReference("refs/tags/v1", r.commits["c3"])))
	return r
}

func TestUploadPackV2DeepenNotNamesMatchCanonical(t *testing.T) {
	t.Parallel()
	r := selectionHistory(t)
	want := "want " + r.commits["m"].String()

	for _, name := range []string{"refs/heads/old", "heads/old", "old", "refs/tags/v1", "tags/v1", "v1"} {
		canonical, got := v2ShallowAnswers(t, r, want, "deepen-not "+name)
		require.False(t, canonical.refused, "git refused deepen-not %s", name)
		assert.Equal(t, canonical, got, "deepen-not %s", name)
	}

	// A name that names nothing, and an object id, which is not a name.
	for _, name := range []string{"nothing", "heads/nothing", r.commits["c2"].String()} {
		canonical, got := v2ShallowAnswers(t, r, want, "deepen-not "+name)
		require.True(t, canonical.refused, "git answered deepen-not %s", name)
		assert.True(t, got.refused, "go-git answered deepen-not %s", name)
	}
}

// A name that is both a branch and a tag is ambiguous, and refused.
func TestUploadPackV2DeepenNotAmbiguousNameMatchesCanonical(t *testing.T) {
	t.Parallel()
	r := selectionHistory(t)
	r.branch("both", "c2")
	require.NoError(t, r.st.SetReference(plumbing.NewHashReference("refs/tags/both", r.commits["c3"])))

	canonical, got := v2ShallowAnswers(t, r, "want "+r.commits["m"].String(), "deepen-not both")
	assert.Equal(t, canonical.refused, got.refused)
	assert.Equal(t, canonical.lines, got.lines)
}

// A wanted annotated tag stands for the commit it peels to: its history is
// cut at the boundary like that of a wanted commit.
func TestUploadPackV2DeepenSinceWithTagWantMatchesCanonical(t *testing.T) {
	t.Parallel()
	r := selectionHistory(t)
	tag := &object.Tag{
		Name: "annotated", Tagger: object.Signature{Name: "t", Email: "t@example.com", When: r.when},
		Target: r.commits["c3"], TargetType: plumbing.CommitObject, Message: "annotated\n",
	}
	o := r.st.NewEncodedObject()
	require.NoError(t, tag.Encode(o))
	tagHash, err := r.st.SetEncodedObject(o)
	require.NoError(t, err)
	require.NoError(t, r.st.SetReference(plumbing.NewHashReference("refs/tags/annotated", tagHash)))

	for _, arg := range []string{"deepen-since 1700002500", "deepen-not old"} {
		canonical, got := v2ShallowAnswers(t, r, "want "+tagHash.String(), arg)
		require.False(t, canonical.refused, "git refused %s", arg)
		require.NotEmpty(t, canonical.lines, arg)
		assert.Equal(t, canonical, got, arg)
	}
}

// A client shallow commit stops being shallow only when the request selects
// it. c2 is reachable from the want c3, but older than the date asked for.
func TestUploadPackV2DeepenSinceKeepsUnselectedClientShallow(t *testing.T) {
	t.Parallel()
	r := selectionHistory(t)

	canonical, got := v2ShallowAnswers(t, r,
		"want "+r.commits["m"].String(), "want "+r.commits["c3"].String(),
		"shallow "+r.commits["c2"].String(), "deepen-since 1700004000")
	require.False(t, canonical.refused, "git refused the request")
	assert.Equal(t, canonical, got)
	assert.NotContains(t, got.lines, "unshallow c2")
}

// A request that selects no commit at all is refused.
func TestUploadPackV2DeepenSelectingNothingMatchesCanonical(t *testing.T) {
	t.Parallel()
	r := selectionHistory(t)

	for _, arg := range []string{"deepen-since 1700003000", "deepen-not refs/heads/main"} {
		canonical, got := v2ShallowAnswers(t, r, "want "+r.commits["c2"].String(), arg)
		require.True(t, canonical.refused, "git answered %s", arg)
		assert.True(t, got.refused, "go-git answered %s with %v", arg, got.lines)
	}
}
