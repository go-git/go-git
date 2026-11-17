package git_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	servergitdaemon "github.com/go-git/go-git/v6/internal/server/git"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestGitServer_PushWithPackfile pushes a commit (so the request carries a
// packfile) to the git:// server. The client does not half-close the TCP
// connection, so a server that reads the pack until EOF never answers.
func TestGitServer_PushWithPackfile(t *testing.T) {
	t.Parallel()

	storers := map[string]func() storage.Storer{
		"filesystem": func() storage.Storer {
			return filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
		},
		"memory": func() storage.Storer { return memory.NewStorage() },
	}

	for name, newStorer := range storers {
		for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
			t.Run(name+"/"+string(of), func(t *testing.T) {
				t.Parallel()

				srvSt := newStorer()
				_, err := gogit.Init(srvSt, gogit.WithObjectFormat(of))
				require.NoError(t, err)

				srv := servergitdaemon.FromLoader(transport.MapLoader{"/repo.git": srvSt})
				endpoint, err := srv.Start()
				require.NoError(t, err)
				t.Cleanup(func() { _ = srv.Close() })

				wt := memfs.New()
				r, err := gogit.Init(memory.NewStorage(), gogit.WithWorkTree(wt), gogit.WithObjectFormat(of))
				require.NoError(t, err)
				require.NoError(t, util.WriteFile(wt, "foo", []byte("bar\n"), 0o644))
				w, err := r.Worktree()
				require.NoError(t, err)
				_, err = w.Add("foo")
				require.NoError(t, err)
				_, err = w.Commit("init", &gogit.CommitOptions{
					Author: &object.Signature{Name: "a", Email: "a@b", When: time.Now()},
				})
				require.NoError(t, err)

				_, err = r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{endpoint + "/repo.git"}})
				require.NoError(t, err)

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				err = r.PushContext(ctx, &gogit.PushOptions{
					RemoteName: "origin",
					RefSpecs:   []config.RefSpec{"refs/heads/master:refs/heads/master"},
				})
				require.NoError(t, err, "push carrying a packfile must complete")
			})
		}
	}
}
