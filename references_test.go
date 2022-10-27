package git

import (
	"bytes"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReferencesSuite struct {
	BaseSuite
}

var _ = Suite(&ReferencesSuite{})

var referencesTests = [...]struct {
	// input data to revlist
	repo   string
	commit string
	path   string
	// expected output data form the revlist
	revs []string
}{
	// Tyba git-fixture
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "binary.jpg", []string{
		"35e85108805c84807bc66a02d91535e1e24b38b9",
		"1669dce138d9b841a518c64b10914d88f5e488ea",
	}},
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "CHANGELOG", []string{
		"b8e471f58bcbca63b07bda20e428190409c2db47",
		"a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
		"1669dce138d9b841a518c64b10914d88f5e488ea",
	}},
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "go/example.go", []string{
		"918c48b83bd081e863dbe1b80f8998f058cd8294",
	}},
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "json/long.json", []string{
		"af2d6a6954d532f8ffb47615169c8fdf9d383a1a",
	}},
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "json/short.json", []string{
		"af2d6a6954d532f8ffb47615169c8fdf9d383a1a",
	}},
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "LICENSE", []string{
		"b029517f6300c2da0f4b651b8642506cd6aaf45d",
	}},
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "php/crappy.php", []string{
		"918c48b83bd081e863dbe1b80f8998f058cd8294",
	}},
	{"https://github.com/git-fixtures/basic.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "vendor/foo.go", []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	}},
	{"https://github.com/jamesob/desk.git", "d4edaf0e8101fcea437ebd982d899fe2cc0f9f7b", "LICENSE", []string{
		"ffcda27c2de6768ee83f3f4a027fa4ab57d50f09",
	}},
	{"https://github.com/jamesob/desk.git", "d4edaf0e8101fcea437ebd982d899fe2cc0f9f7b", "README.md", []string{
		"ffcda27c2de6768ee83f3f4a027fa4ab57d50f09",
		"2e87a2dcc63a115f9a61bd969d1e85fb132a431b",
		"215b0ac06225b0671bc3460d10da88c3406f796f",
		"0260eb7a2623dd2309ab439f74e8681fccdc4285",
		"d46b48933e94f30992486374fa9a6becfd28ea17",
		"b5dfcf27305a30afb2eb1d5b2fdf573b48e5bd23",
		"9cb4df2a88efee8836f9b8ad27ca2717f624164e",
		"d2cb2828d2d58bb672d6f1c96e99abcbfacf8676",
		"8c49acdec2ed441706d8799f8b17878aae4c1ffe",
		"ebaca0c6f54c23193ee8175c3530e370cb2dabe3",
		"77675f82039551a19de4fbccbe69366fe63680df",
		"b9741594fb8ab7374f9be07d6a09a3bf96719816",
		"04db6acd94de714ca48128c606b17ee1149a630e",
		"ff737bd8a962a714a446d7592fae423a56e61e12",
		"eadd03f7a1cc54810bd10eef6747ad9562ad246d",
		"4b465212632b8514f6f692fbbb1c98f0cec7e80c",
		"b5072ab5c1cf89191d71f1244eecc5d1f369ef7e",
		"bfa6ebc9948f1939402b063c0a2a24bf2b1c1cc3",
		"59e1f401534040582e296f09cad02ced8fe0e1ff",
		"d9aef39828c670dfdb172502021a2ebcda8cf2fb",
		"1a6b6e45c91e1831494eb139ee3f8e21649c7fb0",
		"fa73b562d62b4392547569784bf4f5a3324a47d2",
		"52d90f9b513dd3c5330663cba39396e6b8a3ba4e",
		"09fdbe4612066cf63ea46aee43c7cfaaff02ecfb",
		"803bf37847633e2f685a46a27b11facf22efebec",
		"236f6526b1150cc1f1723566b4738f443fc70777",
		"7970ae7cc165c5205945dfb704d67d53031f550a",
		"2e536294f2af0f18785079de8335201684018461",
		"7862953f470b62397d22f6782a884f5bea6d760d",
		"b0b0152d08c2333680266977a5bc9c4e50e1e968",
		"13ce6c1c77c831f381974aa1c62008a414bd2b37",
		"af4e603a8c564558686d430a30928431562e8041",
		"d3f3c8faca048d11709969fbfc0cdf2901b87578",
		"8777dde1abe18c805d021366643218d3f3356dd9",
		"d4edaf0e8101fcea437ebd982d899fe2cc0f9f7b",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "pylib/spinnaker/reconfigure_spinnaker.py", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "pylib/spinnaker/validate_configuration.py", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"1e14f94bcf82694fdc7e2dcbbfdbbed58db0f4d9",
		"edf909edb9319c5e615e4ce73da47bbdca388ebe",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"1e3d328a2cabda5d0aaddc5dec65271343e0dc37",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
		"e3f31c79bba1da4d60f259089199036f6dde355d",
		"427af6949a88f076bb0cd6925071c21be66b41a5",
		"9a8fda3ad2dd768bda41b7f1d6ed5d2eca464f24",
		"b5d999e2986e190d81767cd3cfeda0260f9f6fb8",
		"be54b2b7561c3d9fb8768f524fa197ca1dad981f",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "pylib/spinnaker/fetch.py", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "pylib/spinnaker/yaml_util.py", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"1e14f94bcf82694fdc7e2dcbbfdbbed58db0f4d9",
		"edf909edb9319c5e615e4ce73da47bbdca388ebe",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
		"b5d999e2986e190d81767cd3cfeda0260f9f6fb8",
		"be54b2b7561c3d9fb8768f524fa197ca1dad981f",
		"023d4fb17b76e0fe0764971df8b8538b735a1d67",
		"bb702a749521496ea7e542df78806671d8d8c657",
		"855c220530cb8aa8e9ff2598fc873240bf4a543b",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "dev/build_release.py", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"1e14f94bcf82694fdc7e2dcbbfdbbed58db0f4d9",
		"edf909edb9319c5e615e4ce73da47bbdca388ebe",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"f42771ba298b93a7c4f5b16c5b30ab96c15305a8",
		"98fa29596e058d0cf253fcdfbc769775e1e5f876",
		"c48cf60958007d45f9c4c8746f00f342236825ee",
		"dd52703a50e71891f63fcf05df1f69836f4e7056",
		"d287c606d356e8d978b9673f5445b27a74ea8721",
		"31ff9c2f840cad649832b8138c188740d193b4b5",
		"b80c66c904860d2c400a191b2892fa473c4735c2",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
		"0d9c9cef53af38cefcb6801bb492aaed3f2c9a42",
		"427af6949a88f076bb0cd6925071c21be66b41a5",
		"9a8fda3ad2dd768bda41b7f1d6ed5d2eca464f24",
		"d375f1994ff4d0bdc32d614e698f1b50e1093f14",
		"1f9684d0e81b4c80400677e029a5d483ddfb2027",
		"abad497f11a366548aa95303c8c2f165fe7ae918",
		"6e00810b67f04ca530bafaed08e38c37a62f3447",
		"6986d885626792dee4ef6b7474dfc9230c5bda54",
		"889c1f2bafc2f74258f608a9beab14dd4a70edb9",
		"18fc95490bcee25a4669d9ab7640e729cef32df4",
		"5422a86a10a8c5a1ef6728f5fc8894d9a4c54cb9",
		"0a67d98c7a0eaa27bf6b62450f6f54aadbb961ed",
		"66ac94f0b4442707fb6f695fbed91d62b3bd9d4a",
		"09a4ea729b25714b6368959eea5113c99938f7b6",
		"f5300bb86b22eda66eb4baef6b2a211c85f14690",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "pkg_scripts/postUninstall.sh", []string{
		"ce9f123d790717599aaeb76bc62510de437761be",
		"6d38cc001b806d0161d18f4837b9e5dfcf29a89e",
		"1f9684d0e81b4c80400677e029a5d483ddfb2027",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "install/first_google_boot.sh", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
		"de25f576b888569192e6442b0202d30ca7b2d8ec",
		"e51871f45f3848ec1ed37aab052277198c98fff1",
		"a6a4e6112a5009e53a37f7325620a93db7eadd9c",
		"889c1f2bafc2f74258f608a9beab14dd4a70edb9",
		"18fc95490bcee25a4669d9ab7640e729cef32df4",
		"a596972a661d9a7deca8abd18b52ce1a39516e89",
		"66ac94f0b4442707fb6f695fbed91d62b3bd9d4a",
		"9467ec579708b3c71dd9e5b3906772841c144a30",
		"ebe1cd8da4246d8b9b3f1c4717e99309a00490f6",
		"921a8a191aff8b0333c08ab78803878fdc26e9f5",
		"c4a9091e4076cb740fa46e790dd5b658e19012ad",
		"743a148328362ff93312329de0165fab07641546",
		"6eb5d9c5225224bfe59c401182a2939d6c27fc00",
		"66d1c8f2fa2e32c2c936679c8b10e2134b2ac187",
		"8a594011096b65f5b455254f95d2c7d99ec64c11",
		"495c7118e7cf757aa04eab410b64bfb5b5149ad2",
		"99280af2aaf171fe056400938ae2dbf6d93d3736",
		"46670eb6477c353d837dbaba3cf36c5f8b86f037",
		"dd2d03c19658ff96d371aef00e75e2e54702da0e",
		"0ce1393c24c7083ec7f9f04b4cf461c047ad2192",
		"2a3b1d3b134e937c7bafdab6cc2950e264bf5dee",
		"3b0f2a5fbc354b116452e9f3e366af74ce1f1321",
		"f69376bd065db787894bd2775d447c8d87d3b50c",
		"a57b08a9072f6a865f760551be2a4944f72f804a",
		"7622add2bc8c47d1a37244f39b94bcc187bf671d",
		"c24f0caac157254e480055fb605a71465d13bc00",
		"0777fadf4ca6f458d7071de414f9bd5417911037",
		"c756e09461d071e98b8660818cf42d90c90f2854",
		"e0005f50e22140def60260960b21667f1fdfff80",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "install/install_spinnaker.sh", []string{
		"0d9c9cef53af38cefcb6801bb492aaed3f2c9a42",
		"427af6949a88f076bb0cd6925071c21be66b41a5",
		"9a8fda3ad2dd768bda41b7f1d6ed5d2eca464f24",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "install/install_fake_openjdk8.sh", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "install/install_spinnaker.py", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"37f94770d81232b1895fca447878f68d65aac652",
		"ae8576d17ed71715823ea35217f0460cceebdd65",
		"46c9dcbb55ca3f4735e82ad006e8cae2fdd050d9",
		"491dcace3776dc1f5f1d13a0c8d933ed0e4f79e1",
		"124a88cfda413cb7182ca9c739a284a9e50042a1",
		"0f84364305a6bb71290c6698e9fd45aa35a935be",
		"c48cf60958007d45f9c4c8746f00f342236825ee",
		"eb4faf67a8b775d7985d07a708e3ffeac4273580",
		"eb81417e29bf40bf358231ac601e2141f7a38051",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
		"0d9c9cef53af38cefcb6801bb492aaed3f2c9a42",
		"427af6949a88f076bb0cd6925071c21be66b41a5",
		"01171a8a2e843bef3a574ba73b258ac29e5d5405",
		"19260d46e6cad0b18357d330b9548cf636e4ed54",
		"739d8c6fe16edcb6ef9185dc74197de561b84315",
		"5a7e23fa48950d7d07dd861b63f0f2740a93a78a",
		"9a8fda3ad2dd768bda41b7f1d6ed5d2eca464f24",
		"d33c2d1e350b03fb989eefc612e8c9d5fa7cadc2",
		"cda6cf2be5027889bf94bd4d1c5a171422bf566c",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "install/__init__.py", []string{
		"a24001f6938d425d0e7504bdf5d27fc866a85c3d",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "experimental/docker-compose/docker-compose.yml", []string{
		"fda357835d889595dc39dfebc6181d863cce7d4f",
		"57c59e7144354a76e1beba69ae2f85db6b1727af",
		"7682dff881029c722d893a112a64fea6849a0428",
		"66f1c938c380a4096674b27540086656076a597f",
		"7ef99b88389c744524e123b5053061768c9383df",
		"56dc238f6f397e93f1d1aad702976889c830e8bf",
		"b95e442c064935709e789fa02126f17ddceef10b",
		"f98965a8f42037bd038b86c3401da7e6dfbf4f2e",
		"a56ccc92c9f7b0f9beb0905fbaedbdc5516ca0a3",
		"e3f31c79bba1da4d60f259089199036f6dde355d",
		"427af6949a88f076bb0cd6925071c21be66b41a5",
		"5344429749e8b68b168d2707b7903692436cc2ea",
		"6a31f5d219766b0cec4ea4fbbbfe47bdcdb0ab8e",
		"69fc14f0cd6ae7d6efcc578a3140663e48531593",
		"c197da249529232a016dbf36c28c18dc39561e15",
		"ddaae195b628150233b0a48f50a1674fd9d1a924",
		"9a8fda3ad2dd768bda41b7f1d6ed5d2eca464f24",
		"cda6cf2be5027889bf94bd4d1c5a171422bf566c",
		"7119ad9cf7d4e4d8b059e5337374baae4adc7458",
		"66ac94f0b4442707fb6f695fbed91d62b3bd9d4a",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "unittest/validate_configuration_test.py", []string{
		"1e14f94bcf82694fdc7e2dcbbfdbbed58db0f4d9",
		"edf909edb9319c5e615e4ce73da47bbdca388ebe",
		"89eac7e84400db93b750414a5d52569694b9ed13",
		"1e3d328a2cabda5d0aaddc5dec65271343e0dc37",
		"b0c2aa0faeacbf9fbc144e1a375571f3d3031715",
		"e3f31c79bba1da4d60f259089199036f6dde355d",
		"427af6949a88f076bb0cd6925071c21be66b41a5",
		"9a8fda3ad2dd768bda41b7f1d6ed5d2eca464f24",
	}},
	{"https://github.com/spinnaker/spinnaker.git", "f39d86f59a0781f130e8de6b2115329c1fbe9545", "README.adoc", []string{
		"638f61b3331695f46f1a88095e26dea0f09f176b",
		"67f0a0f488b3592bb611391150f2e1d0ee037231",
		"bd42370d3fe8d410e78acb96f81cb3d838ad1c21",
		"d3046b5b2f7aafa0832da6806ee8c7dab7d0da9e",
		"f5300bb86b22eda66eb4baef6b2a211c85f14690",
		"d6905eab6fec1841c7cf8e4484499f5c8d7d423e",
		"c0a70a0f5aa494f0ae01c55ba191f2325556489a",
		"811795c8a185e88f5d269195cb68b29c8d0fe170",
		"d6e6fe0194447cc280f942d6a2e0521b68ea7796",
		"9414750a933037ec4f0bc42af7ad81ec4f360c0a",
		"6ea37d18b706aab813532254ce0d412843c68782",
		"174bdbf9edfb0ca88415dd4a673852d5b22e7036",
		"6694fb99ca6fbf469798f1fb9386b55ff80f0128",
		"9944d6cf72b8f82d622d85dad7434472bc8f397d",
		"d7a3eedbf9fa133d7c4366afae555a2ed46d4849",
		"e805183c72f0426fb073728c01901c2fd2db1da6",
		"4f9cd01b6e533c3b1261660b9cc3302879e5b303",
		"b45ffa99a6daaf045043ab0b0d8bcf823f10e157",
		"8a9804234551d61209f67b3c89f7706f248ae805",
		"8ef83dd443a05e9122681950399edaa58a38d466",
		"cfdd19354e2a3981484a7cfe4b0d95c9abce9296",
		"608976766959bdb1b18eaa53b3ca33ee6782bc3c",
		"b260ce026a2505037876b4c21c0985882ff373b7",
		"d73f9cee49a5ad27a42a6e18af7c49a8f28ad8a8",
		"a74422026841e05debdcc417190428b419a99f39",
		"4f3c7375fa7c661735a6a69beeeeac1aaa43f7c9",
	}},
	// FAILS
	/*
		// this contains an empty move
		{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "google/dev/build_google_tarball.py", []string{
			"88e60ac93f832efc2616b3c165e99a8f2ffc3e0c",
			"9e49443da49b8c862cc140b660744f84eebcfa51",
		}},
	*/
	/*
		{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "unittest/yaml_util_test.py", []string{
			"edf909edb9319c5e615e4ce73da47bbdca388ebe",
			"023d4fb17b76e0fe0764971df8b8538b735a1d67",
		}},
	*/
	/*
		{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "unittest/configurator_test.py", []string{
			"1e14f94bcf82694fdc7e2dcbbfdbbed58db0f4d9",
			"edf909edb9319c5e615e4ce73da47bbdca388ebe",
			"d14f793a6cd7169ef708a4fc276ad876bd3edd4e",
			"023d4fb17b76e0fe0764971df8b8538b735a1d67",
		}},
	*/
	/*
		// this contains a cherry-pick at 094d0e7d5d691  (with 3f34438d)
		{"https://github.com/jamesob/desk.git", "d4edaf0e8101fcea437ebd982d899fe2cc0f9f7b", "desk", []string{
			"ffcda27c2de6768ee83f3f4a027fa4ab57d50f09",
			"a0c1e853158ccbaf95574220bbf3b54509034a9f",
			"decfc524570c407d6bba0f217e534c8b47dbdbee",
			"1413872d5b3af7cd674bbe0e1f23387cd5d940e6",
			"40cd5a91d916e7b2f331e4e85fdc52636fd7cff7",
			"8e07d73aa0e3780f8c7cf8ad1a6b263df26a0a52",
			"19c56f95720ac3630efe9f29b1a252581d6cbc0c",
			"9ea46ccc6d253cffb4b7b66e936987d87de136e4",
			"094d0e7d5d69141c98a606910ba64786c5565da0",
			"801e62706a9e4fef75fcaca9c78744de0bc36e6a",
			"eddf335f31c73624ed3f40dc5fcad50136074b2b",
			"c659093f06eb2bd68c6252caeab605e5cd8aa49e",
			"d94b3fe8ce0e3a474874d742992d432cd040582f",
			"93cddf036df2d8509f910063696acd556ca7600f",
			"b3d4cb0c826b16b301f088581d681654d8de6c07",
			"52d90f9b513dd3c5330663cba39396e6b8a3ba4e",
			"15919e99ded03c6ceea9ff98558e77a322a4dadb",
			"803bf37847633e2f685a46a27b11facf22efebec",
			"c07ad524ee1e616c70bf2ea7a0ee4f4a01195d78",
			"b91aff30f318fda461d009c308490613b394f3e2",
			"67cec1e8a3f21c6eb11678e3f31ffd228b55b783",
			"bbe404c78af7525fabc57b9e7aa7c100b0d39f7a",
			"5dd078848786c2babc2511e9502fa98518cf3535",
			"7970ae7cc165c5205945dfb704d67d53031f550a",
			"33091ac904747747ff30f107d4d0f22fa872eccf",
			"069f81cab12d185ba1b509be946c47897cd4fb1f",
			"13ce6c1c77c831f381974aa1c62008a414bd2b37",
		}},
	*/
	/*
		{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "InstallSpinnaker.sh", []string{
			"ce9f123d790717599aaeb76bc62510de437761be",
			"23673af3ad70b50bba7fdafadc2323302f5ba520",
			"b7015a5d36990d69a054482556127b9c7404a24a",
			"582da9622e3a72a19cd261a017276d72b5b0051a",
			"0c5bb1e4392e751f884f3c57de5d4aee72c40031",
			"c9c2a0ec03968ab17e8b16fdec9661eb1dbea173",
			"a3cdf880826b4d9af42b93f4a2df29a91ab31d35",
			"18526c447f5174d33c96aac6d6433318b0e2021c",
			"2a6288be1c8ea160c443ca3cd0fe826ff2387d37",
			"9e74d009894d73dd07773ea6b3bdd8323db980f7",
			"d2f6214b625db706384b378a29cc4c22237db97a",
			"202a9c720b3ba8106e022a0ad027ebe279040c78",
			"791bcd1592828d9d5d16e83f3a825fb08b0ba22d",
			"01e65d67eed8afcb67a6bdf1c962541f62b299c9",
			"6328ee836affafc1b52127147b5ca07300ac78e6",
			"3de4f77c105f700f50d9549d32b9a05a01b46c4b",
			"8980daf661408a3faa1f22c225702a5c1d11d5c9",
			"8eb116de9128c314ac8a6f5310ca500b8c74f5db",
			"88e841aad37b71b78a8fb88bc75fe69499d527c7",
			"370d61cdbc1f3c90db6759f1599ccbabd40ad6c1",
			"505577dc87d300cf562dc4702a05a5615d90d855",
			"b5c6053a46993b20d1b91e7b7206bffa54669ad7",
			"ba486de7c025457963701114c683dcd4708e1dee",
			"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941",
			"a47d0aaeda421f06df248ad65bd58230766bf118",
			"495c7118e7cf757aa04eab410b64bfb5b5149ad2",
			"46670eb6477c353d837dbaba3cf36c5f8b86f037",
			"dd2d03c19658ff96d371aef00e75e2e54702da0e",
			"4bbcad219ec55a465fb48ce236cb10ca52d43b1f",
			"50d0556563599366f29cb286525780004fa5a317",
			"9a06d3f20eabb254d0a1e2ff7735ef007ccd595e",
			"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73",
		}},
	*/
	/*
		{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "config/default-spinnaker-local.yml", []string{
			"ae904e8d60228c21c47368f6a10f1cc9ca3aeebf",
			"99534ecc895fe17a1d562bb3049d4168a04d0865",
			"caf6d62e8285d4681514dd8027356fb019bc97ff",
			"eaf7614cad81e8ab5c813dd4821129d0c04ea449",
			"5a2a845bc08974a36d599a4a4b7e25be833823b0",
			"41e96c54a478e5d09dd07ed7feb2d8d08d8c7e3c",
			"974b775a8978b120ff710cac93a21c7387b914c9",
			"87e459a9a044b3109dfeb943cc82c627b61d84a6",
			"5e09821cbd7d710405b61cab0a795c2982a71b9c",
			"8cc2d4bdb0a15aafc7fe02cdcb03ab90c974cafa",
			"3ce7b902a51bac2f10994f7d1f251b616c975e54",
			"a596972a661d9a7deca8abd18b52ce1a39516e89",
			"8980daf661408a3faa1f22c225702a5c1d11d5c9",
		}},
	*/
	/*
		{"https://github.com/spinnaker/spinnaker.git", "b32b2aecae2cfca4840dd480f8082da206a538da", "config/spinnaker.yml", []string{
			"ae904e8d60228c21c47368f6a10f1cc9ca3aeebf",
			"caf6d62e8285d4681514dd8027356fb019bc97ff",
			"eaf7614cad81e8ab5c813dd4821129d0c04ea449",
			"5a2a845bc08974a36d599a4a4b7e25be833823b0",
			"41e96c54a478e5d09dd07ed7feb2d8d08d8c7e3c",
			"974b775a8978b120ff710cac93a21c7387b914c9",
			"ed887f6547d7cd2b2d741184a06f97a0a704152b",
			"d4553dac205023fa77652308af1a2d1cf52138fb",
			"a596972a661d9a7deca8abd18b52ce1a39516e89",
			"66ac94f0b4442707fb6f695fbed91d62b3bd9d4a",
			"079e42e7c979541b6fab7343838f7b9fd4a360cd",
		}},
	*/
}

func (s *ReferencesSuite) TestObjectNotFoundError(c *C) {
	h1 := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")
	hParent := plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")

	url := fixtures.ByURL("https://github.com/git-fixtures/basic.git").One().DotGit().Root()
	storer := memory.NewStorage()
	r, err := Clone(storer, nil, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	delete(storer.Objects, hParent)

	commit, err := r.CommitObject(h1)
	c.Assert(err, IsNil)

	_, err = references(commit, "LICENSE")
	c.Assert(err, Equals, plumbing.ErrObjectNotFound)
}

func (s *ReferencesSuite) TestRevList(c *C) {
	for _, t := range referencesTests {
		r := s.NewRepositoryFromPackfile(fixtures.ByURL(t.repo).One())

		commit, err := r.CommitObject(plumbing.NewHash(t.commit))
		c.Assert(err, IsNil)

		revs, err := references(commit, t.path)
		c.Assert(err, IsNil)
		c.Assert(len(revs), Equals, len(t.revs))

		for i := range revs {
			if revs[i].Hash.String() != t.revs[i] {
				commit, err := s.Repository.CommitObject(plumbing.NewHash(t.revs[i]))
				c.Assert(err, IsNil)
				equiv, err := equivalent(t.path, revs[i], commit)
				c.Assert(err, IsNil)
				if equiv {
					fmt.Printf("cherry-pick detected: %s    %s\n", revs[i].Hash.String(), t.revs[i])
				} else {
					c.Fatalf("\nrepo=%s, commit=%s, path=%s, \n%s",
						t.repo, t.commit, t.path, compareSideBySide(t.revs, revs))
				}
			}
		}
	}
}

// same length is assumed
func compareSideBySide(a []string, b []*object.Commit) string {
	var buf bytes.Buffer
	buf.WriteString("\t              EXPECTED                                          OBTAINED        ")
	var sep string
	var obt string
	for i := range a {
		obt = b[i].Hash.String()
		if a[i] != obt {
			sep = "------"
		} else {
			sep = "      "
		}
		buf.WriteString(fmt.Sprintf("\n%d", i+1))
		buf.WriteString(sep)
		buf.WriteString(a[i])
		buf.WriteString(sep)
		buf.WriteString(obt)
	}
	return buf.String()
}

var cherryPicks = [...][]string{
	// repo, path, commit a, commit b
	{"https://github.com/jamesob/desk.git", "desk", "094d0e7d5d69141c98a606910ba64786c5565da0", "3f34438d54f4a1ca86db8c0f03ed8eb38f20e22c"},
}

// should detect cherry picks
func (s *ReferencesSuite) TestEquivalent(c *C) {
	for _, t := range cherryPicks {
		cs := s.commits(c, t[0], t[2], t[3])
		equiv, err := equivalent(t[1], cs[0], cs[1])
		c.Assert(err, IsNil)
		c.Assert(equiv, Equals, true, Commentf("repo=%s, file=%s, a=%s b=%s", t[0], t[1], t[2], t[3]))
	}
}

// returns the commits from a slice of hashes
func (s *ReferencesSuite) commits(c *C, repo string, hs ...string) []*object.Commit {
	r := s.NewRepositoryFromPackfile(fixtures.ByURL(repo).One())

	result := make([]*object.Commit, 0, len(hs))
	for _, h := range hs {
		commit, err := r.CommitObject(plumbing.NewHash(h))
		c.Assert(err, IsNil)

		result = append(result, commit)
	}

	return result
}
