package commitgraph_test

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

func TestOpenChainFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		goodShas []string
		badShas  []string
	}{
		{
			name:     "sha1",
			goodShas: sha1Data,
			badShas:  sha1Invalid,
		},
		{
			name:     "sha256",
			goodShas: sha256Data,
			badShas:  sha256Invalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			option := commitgraph.WithObjectFormat(formatcfg.ObjectFormat(tc.name))

			chainData := strings.Join(tc.goodShas, "\n") + "\n"
			chainReader := strings.NewReader(chainData)

			chain, err := commitgraph.OpenChainFile(chainReader, option)
			require.NoError(t, err)
			assert.Equal(t, chain, tc.goodShas)

			// Test with bad shas
			chainData = strings.Join(tc.badShas, "\n") + "\n"

			chainReader = strings.NewReader(chainData)

			chain, err = commitgraph.OpenChainFile(chainReader, option)
			require.ErrorIs(t, err, commitgraph.ErrMalformedCommitGraphFile)
			assert.Nil(t, chain)

			// Test with empty file
			emptyChainReader := bytes.NewReader(nil)

			chain, err = commitgraph.OpenChainFile(emptyChainReader, option)
			require.NoError(t, err)
			assert.Equal(t, []string{}, chain)

			// Test with file containing only newlines
			newlineChainData := []byte("\n\n\n")
			newlineChainReader := bytes.NewReader(newlineChainData)

			chain, err = commitgraph.OpenChainFile(newlineChainReader, option)
			require.ErrorIs(t, err, commitgraph.ErrMalformedCommitGraphFile)
			assert.Nil(t, chain)
		})
	}
}

func TestOpenChainIndexBrokenLayer(t *testing.T) {
	t.Parallel()

	const tip = "31eae7b619d166c366bf5df4991f04ba8cebea0a"

	// A chain names each layer by its trailing checksum, so the valid
	// base must be named after the graph it holds.
	baseGraph := encodeTestGraph(t)
	base := hex.EncodeToString(baseGraph[len(baseGraph)-sha1.Size:])

	tests := []struct {
		name   string
		chain  []string
		graphs map[string][]byte
		err    error
	}{
		{
			name:  "missing graph",
			chain: []string{tip},
			err:   os.ErrNotExist,
		},
		{
			name:   "corrupt graph",
			chain:  []string{tip},
			graphs: map[string][]byte{tip: []byte("not a graph")},
			err:    commitgraph.ErrMalformedCommitGraphFile,
		},
		{
			name:   "missing graph after valid base",
			chain:  []string{base, tip},
			graphs: map[string][]byte{base: baseGraph},
			err:    os.ErrNotExist,
		},
		{
			name:  "corrupt graph after valid base",
			chain: []string{base, tip},
			graphs: map[string][]byte{
				base: baseGraph,
				tip:  []byte("not a graph"),
			},
			err: commitgraph.ErrMalformedCommitGraphFile,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := &closeTrackingFS{Filesystem: memfs.New()}
			dir := path.Join("objects", "info", "commit-graphs")
			chain := strings.Join(tc.chain, "\n") + "\n"
			require.NoError(t, util.WriteFile(fs, path.Join(dir, "commit-graph-chain"), []byte(chain), 0o644))
			for hash, data := range tc.graphs {
				require.NoError(t, util.WriteFile(fs, path.Join(dir, "graph-"+hash+".graph"), data, 0o644))
			}

			index, err := commitgraph.OpenChainIndex(fs)
			require.ErrorIs(t, err, tc.err)
			assert.Nil(t, index)
			assert.Zero(t, fs.open, "files left open")
		})
	}
}

func TestOpenChainIndexNoLayers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chain string
	}{
		{
			name:  "empty",
			chain: "",
		},
		{
			name:  "unterminated hash",
			chain: "c336d16298a017486c4164c40f8acb28afe64e84",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := &closeTrackingFS{Filesystem: memfs.New()}
			chainPath := path.Join("objects", "info", "commit-graphs", "commit-graph-chain")
			require.NoError(t, util.WriteFile(fs, chainPath, []byte(tc.chain), 0o644))

			index, err := commitgraph.OpenChainIndex(fs)
			require.ErrorIs(t, err, commitgraph.ErrMalformedCommitGraphFile)
			assert.Nil(t, index)

			index, err = commitgraph.OpenChainOrFileIndex(fs)
			require.ErrorIs(t, err, commitgraph.ErrMalformedCommitGraphFile)
			assert.Nil(t, index)

			assert.Zero(t, fs.open, "files left open")
		})
	}
}

// closeTrackingFS counts the files opened through it that are not yet
// closed.
type closeTrackingFS struct {
	billy.Filesystem
	open int
}

func (fs *closeTrackingFS) Open(name string) (billy.File, error) {
	f, err := fs.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	fs.open++
	return &closeTrackingFile{File: f, fs: fs}, nil
}

type closeTrackingFile struct {
	billy.File
	fs *closeTrackingFS
}

func (f *closeTrackingFile) Close() error {
	f.fs.open--
	return f.File.Close()
}

func encodeTestGraph(t *testing.T) []byte {
	t.Helper()

	idx := commitgraph.NewMemoryIndex()
	idx.Add(plumbing.NewHash("347c91919944a68e9413581a1bc15519550a3afe"), &commitgraph.CommitData{
		TreeHash:   plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"),
		Generation: 1,
		When:       time.Unix(1, 0),
	})

	var buf bytes.Buffer
	require.NoError(t, commitgraph.NewEncoder(&buf).Encode(idx))
	return buf.Bytes()
}

func TestOpenChainFileIgnoresUnterminatedEntry(t *testing.T) {
	t.Parallel()
	first := strings.Repeat("a", 40)
	last := strings.Repeat("b", 40)
	for _, tc := range []struct {
		name string
		data string
		want []string
	}{
		{"terminated", first + "\n" + last + "\n", []string{first, last}},
		{"unterminated", first + "\n" + last, []string{first}},
		{"single unterminated", last, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			chain, err := commitgraph.OpenChainFile(strings.NewReader(tc.data))
			require.NoError(t, err)
			require.Equal(t, tc.want, chain)
		})
	}
}

var (
	sha1Data = []string{
		"c336d16298a017486c4164c40f8acb28afe64e84",
		"31eae7b619d166c366bf5df4991f04ba8cebea0a",
		"b977a025ca21e3b5ca123d8093bd7917694f6da7",
		"d2a38b4a5965d529566566640519d03d2bd10f6c",
		"35b585759cbf29f8ec428ef89da20705d59f99ec",
		"c2bbf9fe8009b22d0f390f3c8c3f13937067590f",
		"fc9f0643b21cfe571046e27e0c4565f3a1ee96c8",
		"c088fd6a7e1a38e9d5a9815265cb575bb08d08ff",
		"5fddbeb678bd2c36c5e5c891ab8f2b143ced5baf",
		"5d7303c49ac984a9fec60523f2d5297682e16646",
	}

	sha1Invalid = []string{
		"5d7303c49ac984a9fec60523f2d5297682e1664x",
	}

	sha256Data = []string{
		"b9efda7160f2647e0974ca623f8a8f8e25fb6944f1b8f78f4db1bf07932de8eb",
		"7095c59f8bf46e12c21d2d9da344cfe383fae18d26f3ae4d4ab7b71e3d0ddfae",
		"25a395cb62f7656294e40a001ee19fefcdf3013d265dfcf4b744cd2549891dec",
		"7fbd564813a82227507d9dd70f1fd21fc1f180223cd3f42e0c3090c9a8b6a7d0",
		"aa95db1db2df91bd7200a892dd1c03bc2704c4793400d016b3ca08c148b0f7c1",
		"2176988184b570565dc33823a02f474ad59f667a0e971c86063a7fea64776a87",
		"d0afc0e64171140eb7902110f807a1beaa38a603d4312fd4bd14a5db2784ba62",
		"2822136f60bfc58bbd9d624cc19fbef9f0fc0efe2a61729242e1e5f9b77fa3d0",
		"6f207b5c43463af96bc38c43b0bf45275fa327e656a8bba8e7fc55c5ab6870d8",
		"6cf33782619b6ff0af9c081e46323f423f8b49bf3d043887c0549bef47d60f55",
		"60ea0753d2d4e828983528294be3f57e2a3ba37df4f59e3236133c9e2b17afc5",
		"6b3c9f4ba5092e0807774097953ec6e9f58e8371d775bd8738a0fa98d728ba3d",
		"c97cab8564054e30515dbe67dda4e14638aabf17b3f042d18dc8461cd098b362",
		"9f7ece76fd2c9dae08e75176347efffc1446ad74af66004dd34680edb205dfb5",
		"23e7a7e481b00571b63c2a7d0432f9733dd85d18a9841a3d7b96743100da5824",
		"e684b1253fa8eb6572f35bab2fd3b6efecabf8472ede43497cd9c171973cc341",
		"8b9f04080b0c40f7ad2a6bb5e5296cd6c06e730dffce87a0375ae7bd0f85f86e",
		"384a745f3b14edc89526a98b96b3247b2b548541c755aadee7664352ed7f12ae",
		"b68c8a82cd5b839917e1058570a0408819b81d16dbab81db118cc8dfc3def044",
		"fbaf04f1a401335be57e172f4326102c658d857fde6cf2bc987520d11fc99770",
		"57acf2aa5ac736337b120c951536c8a2b2cb23a4f0f198e86f3433370fa63105",
		"dd7fcba4c13b6ced0b6190cdb5861adcd08446a92d67f7ec0f02f9533e09bbb0",
		"744ef481c9b13ebd3b6e43d7e9ba25f7c7a5c8e453e6f0d50f5d71aae1591689",
		"2c573142f1edd52b64dcd42a9c3b0ca5c9c615f757d80d25bfb02ff3eb2257e2",
		"ea65cc58ef8520cd0335de4318a0d3b3a1ac257b7e9f82e12483fa3bce6cc0cd",
		"1dfa626ff1523b82e21a4c29476edcdc9a89842f3c7181f63a28cd4f46cc9923",
		"aa1153e71af836121e6f6cc716cf64880c19221d8dc367ff42359de1b8ef30e9",
		"a7c6ec6f6569e22d2fa6e8281639d27c59b633ea00ad8ef27a43171cc985fbda",
		"627b706d63d2cfd5a388deeaa76655ef09146fe492ee17cb0043578cef9c2800",
		"d40eaf091ef8357b734d1047a552436eaf057d99a0c6f2068b097c324099d360",
		"87f0ef81641da4fd3438dcaae4819f0c92a0ade54e262b21f9ded4575ff3f234",
		"3a00a29e08d29454b5197662f70ccab5699b0ce8c85af7fbf511b8915d97cfd0",
	}

	sha256Invalid = []string{
		"3a00a29e08d29454b5197662f70ccab5699b0ce8c85af7fbf511b8915d97cfdx",
	}
)
