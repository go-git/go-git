package git

import (
	"context"
	"fmt"
	"time"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage/memory"
)

// A stateful v0 client writes a round of haves before it has read the answer
// to the round before. Against the in-process file transport that only works
// if the server can answer without the client reading: with more haves than
// fit in the first round, the fetch would otherwise hang.
func (s *RemoteSuite) TestFetchV0OverFileWithMoreHavesThanOneRound() {
	for _, protocolVersion := range []protocol.Version{protocol.V0, protocol.V1} {
		s.Run(protocolVersion.String(), func() {
			st := memory.NewStorage()
			cfg, err := st.Config()
			s.Require().NoError(err)
			cfg.Protocol.Version = protocolVersion
			s.Require().NoError(st.SetConfig(cfg))

			// 100 local commits the server lacks: several rounds of haves.
			sig := object.Signature{Name: "t", Email: "t@example.com"}
			var parent plumbing.Hash
			for i := range 100 {
				sig.When = time.Unix(int64(1700000000+i), 0).UTC()
				c := &object.Commit{Author: sig, Committer: sig, Message: fmt.Sprintf("local %d", i)}
				if !parent.IsZero() {
					c.ParentHashes = []plumbing.Hash{parent}
				}
				o := st.NewEncodedObject()
				s.Require().NoError(c.Encode(o))
				parent, err = st.SetEncodedObject(o)
				s.Require().NoError(err)
			}
			s.Require().NoError(st.SetReference(plumbing.NewHashReference("refs/heads/local", parent)))

			r := NewRemote(st, &config.RemoteConfig{
				Name: DefaultRemoteName,
				URLs: []string{s.GetBasicLocalRepositoryURL()},
			})

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = r.FetchContext(ctx, &FetchOptions{
				RefSpecs: []config.RefSpec{"+refs/heads/master:refs/remotes/origin/master"},
			})
			s.Require().NoError(err)

			ref, err := st.Reference("refs/remotes/origin/master")
			s.Require().NoError(err)
			s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", ref.Hash().String())
		})
	}
}
