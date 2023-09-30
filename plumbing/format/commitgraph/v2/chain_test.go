package v2_test

import (
	"bytes"
	"crypto"
	"strings"

	commitgraph "github.com/go-git/go-git/v5/plumbing/format/commitgraph/v2"
	"github.com/go-git/go-git/v5/plumbing/hash"

	. "gopkg.in/check.v1"
)

func (s *CommitgraphSuite) TestOpenChainFile(c *C) {
	sha1Data := []string{
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

	sha256Data := []string{
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

	goodShas := sha1Data
	badShas := sha256Data
	if hash.CryptoType == crypto.SHA256 {
		goodShas = sha256Data
		badShas = sha1Data
	}
	chainData := strings.Join(goodShas, "\n") + "\n"

	chainReader := strings.NewReader(chainData)

	chain, err := commitgraph.OpenChainFile(chainReader)
	c.Assert(err, IsNil)
	c.Assert(goodShas, DeepEquals, chain)

	// Test with bad shas
	chainData = strings.Join(badShas, "\n") + "\n"

	chainReader = strings.NewReader(chainData)

	chain, err = commitgraph.OpenChainFile(chainReader)
	c.Assert(err, Equals, commitgraph.ErrMalformedCommitGraphFile)
	c.Assert(chain, IsNil)

	// Test with empty file
	emptyChainReader := bytes.NewReader(nil)

	chain, err = commitgraph.OpenChainFile(emptyChainReader)
	c.Assert(err, IsNil)
	c.Assert(chain, DeepEquals, []string{})

	// Test with file containing only newlines
	newlineChainData := []byte("\n\n\n")
	newlineChainReader := bytes.NewReader(newlineChainData)

	chain, err = commitgraph.OpenChainFile(newlineChainReader)
	c.Assert(err, Equals, commitgraph.ErrMalformedCommitGraphFile)
	c.Assert(chain, IsNil)
}
