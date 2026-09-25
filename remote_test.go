package git

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

type RemoteSuite struct {
	BaseSuite
}

func TestRemoteSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(RemoteSuite))
}

func (s *RemoteSuite) TestString() {
	r := NewRemote(nil, &config.RemoteConfig{
		Name: "foo",
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})

	s.Equal(""+
		"foo\thttps://github.com/git-fixtures/basic.git (fetch)\n"+
		"foo\thttps://github.com/git-fixtures/basic.git (push)",
		r.String(),
	)
}

func (s *RemoteSuite) TestList() {
	repo := fixtures.Basic().One()
	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{repo.URL},
	})

	refs, err := remote.List(&ListOptions{})
	s.NoError(err)

	expected := []*plumbing.Reference{
		plumbing.NewSymbolicReference("HEAD", "refs/heads/master"),
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/heads/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/pull/1/head", "b8e471f58bcbca63b07bda20e428190409c2db47"),
		plumbing.NewReferenceFromStrings("refs/pull/2/head", "9632f02833b2f9613afb5e75682132b0b22e4a31"),
		plumbing.NewReferenceFromStrings("refs/pull/2/merge", "c37f58a130ca555e42ff96a071cb9ccb3f437504"),
	}
	s.Len(expected, len(refs))
	for _, e := range expected {
		found := false
		for _, r := range refs {
			if r.Name() == e.Name() {
				found = true
				s.Equal(e, r)
			}
		}
		s.True(found)
	}
}

func (s *RemoteSuite) TestListPeeling() {
	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"https://github.com/git-fixtures/tags.git"},
	})

	for _, tc := range []struct {
		peelingOption   PeelingOption
		expectPeeled    bool
		expectNonPeeled bool
	}{
		{peelingOption: AppendPeeled, expectPeeled: true, expectNonPeeled: true},
		{peelingOption: IgnorePeeled, expectPeeled: false, expectNonPeeled: true},
		{peelingOption: OnlyPeeled, expectPeeled: true, expectNonPeeled: false},
	} {
		refs, err := remote.List(&ListOptions{
			PeelingOption: tc.peelingOption,
		})
		s.NoError(err)
		s.True(len(refs) > 0)

		foundPeeled, foundNonPeeled := false, false
		for _, ref := range refs {
			if strings.HasSuffix(ref.Name().String(), peeledSuffix) {
				foundPeeled = true
			} else {
				foundNonPeeled = true
			}
		}

		comment := fmt.Sprintf("PeelingOption: %v", tc.peelingOption)
		s.Equal(tc.expectPeeled, foundPeeled, comment)
		s.Equal(tc.expectNonPeeled, foundNonPeeled, comment)
	}
}

func (s *RemoteSuite) TestListTimeout() {
	// Create a server that blocks until the request context is done
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{srv.URL},
	})

	_, err := remote.ListContext(ctx, &ListOptions{})
	s.ErrorIs(err, context.DeadlineExceeded)
}

// A remote is free to advertise a name go-git will not store. Dropping it
// where the advertisement becomes a list is what keeps one such name from
// costing the whole fetch, and mirrors filter_refs in fetch-pack.c.
func (s *RemoteSuite) TestReferenceStorageFromRefsDropsUnusableNames() {
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	refs := make([]*plumbing.Reference, 0, 8)
	for _, n := range []plumbing.ReferenceName{
		"HEAD",
		"refs/heads/main",
		"refs/heads/@",
		"refs/heads/-foo",
		"refs/heads/stale.lock",
		"refs/heads/bad~name",
		"refs/heads/.hidden",
		"refs/heads/a..b",
	} {
		refs = append(refs, plumbing.NewHashReference(n, hash))
	}

	got := referenceStorageFromRefs(refs, true)

	for _, n := range []plumbing.ReferenceName{"HEAD", "refs/heads/main", "refs/heads/@", "refs/heads/-foo"} {
		_, err := got.Reference(n)
		s.NoError(err, "%q must survive", n)
	}
	for _, n := range []plumbing.ReferenceName{
		"refs/heads/stale.lock", "refs/heads/bad~name",
		"refs/heads/.hidden", "refs/heads/a..b",
	} {
		_, err := got.Reference(n)
		s.ErrorIs(err, plumbing.ErrReferenceNotFound, "%q must be dropped", n)
	}
}
