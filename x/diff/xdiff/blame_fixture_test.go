package xdiff_test

import (
	"context"
	"reflect"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/storage/memory"

	"github.com/go-git/go-git/v6/x/diff/xdiff"
)

func TestBlameInstallSpinnaker(t *testing.T) {
	t.Parallel()
	fixture := fixtures.ByURL("https://github.com/spinnaker/spinnaker.git").One()
	pack, err := fixture.Packfile()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pack.Close() }()
	storage := memory.NewStorage()
	if err := packfile.UpdateObjectStorage(storage, pack); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetReference(plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(fixture.Head))); err != nil {
		t.Fatal(err)
	}
	repo, err := git.Open(storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	commit, err := repo.CommitObject(plumbing.NewHash("f39d86f59a0781f130e8de6b2115329c1fbe9545"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "InstallSpinnaker.sh"
	result, err := git.BlameWithOptions(context.Background(), commit, path,
		&git.BlameOptions{Differ: xdiff.New(nil)})
	if err != nil {
		t.Fatal(err)
	}
	file, err := commit.File(path)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := file.Lines()
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != path || result.Rev != commit.Hash || len(result.Lines) != len(lines) {
		t.Fatalf("unexpected blame result: path=%s rev=%s lines=%d",
			result.Path, result.Rev, len(result.Lines))
	}
	line := 0
	for _, run := range spinnakerBlameRuns {
		source, err := repo.CommitObject(plumbing.NewHash(run.hash))
		if err != nil {
			t.Fatal(err)
		}
		for range run.lines {
			if line >= len(result.Lines) {
				t.Fatal("expected blame runs exceed file length")
			}
			want := &git.Line{
				Author: source.Author.Email, AuthorName: source.Author.Name,
				Text: lines[line], Date: source.Author.When, Hash: source.Hash,
			}
			if !reflect.DeepEqual(result.Lines[line], want) {
				t.Fatalf("line %d: got %+v, want %+v", line+1, result.Lines[line], want)
			}
			line++
		}
	}
	if line != len(result.Lines) {
		t.Fatalf("expected blame runs cover %d of %d lines", line, len(result.Lines))
	}
}

// The added echo_status function must not change the preceding brace's attribution.
var spinnakerBlameRuns = []struct {
	hash  string
	lines int
}{
	{"ce9f123d790717599aaeb76bc62510de437761be", 2},
	{"a47d0aaeda421f06df248ad65bd58230766bf118", 1},
	{"23673af3ad70b50bba7fdafadc2323302f5ba520", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 29},
	{"9a06d3f20eabb254d0a1e2ff7735ef007ccd595e", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 4},
	{"a47d0aaeda421f06df248ad65bd58230766bf118", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 5},
	{"0c5bb1e4392e751f884f3c57de5d4aee72c40031", 2},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 3},
	{"ce9f123d790717599aaeb76bc62510de437761be", 7},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 2},
	{"ce9f123d790717599aaeb76bc62510de437761be", 5},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 7},
	{"ce9f123d790717599aaeb76bc62510de437761be", 3},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 6},
	{"ce9f123d790717599aaeb76bc62510de437761be", 10},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 4},
	{"0c5bb1e4392e751f884f3c57de5d4aee72c40031", 2},
	{"ce9f123d790717599aaeb76bc62510de437761be", 2},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 4},
	{"23673af3ad70b50bba7fdafadc2323302f5ba520", 4},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 4},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 1},
	{"0c5bb1e4392e751f884f3c57de5d4aee72c40031", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 13},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 2},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 6},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 2},
	{"0c5bb1e4392e751f884f3c57de5d4aee72c40031", 1},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 4},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 3},
	{"ce9f123d790717599aaeb76bc62510de437761be", 2},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 4},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 3},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 15},
	{"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941", 1},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 1},
	{"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941", 8},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 2},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 12},
	{"505577dc87d300cf562dc4702a05a5615d90d855", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 5},
	{"370d61cdbc1f3c90db6759f1599ccbabd40ad6c1", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 4},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 1},
	{"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941", 5},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 3},
	{"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941", 2},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 2},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 9},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 1},
	{"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941", 3},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 4},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"8eb116de9128c314ac8a6f5310ca500b8c74f5db", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 6},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 6},
	{"d2f6214b625db706384b378a29cc4c22237db97a", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 3},
	{"d2f6214b625db706384b378a29cc4c22237db97a", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 4},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 1},
	{"c9c2a0ec03968ab17e8b16fdec9661eb1dbea173", 1},
	{"d2f6214b625db706384b378a29cc4c22237db97a", 2},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 12},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 5},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 3},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 5},
	{"ce9f123d790717599aaeb76bc62510de437761be", 3},
	{"a47d0aaeda421f06df248ad65bd58230766bf118", 5},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 5},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 2},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 1},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"b2c7142082d52b09ca20228606c31c7479c0833e", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"495c7118e7cf757aa04eab410b64bfb5b5149ad2", 1},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 1},
	{"495c7118e7cf757aa04eab410b64bfb5b5149ad2", 3},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 1},
	{"495c7118e7cf757aa04eab410b64bfb5b5149ad2", 1},
	{"50d0556563599366f29cb286525780004fa5a317", 1},
	{"dd2d03c19658ff96d371aef00e75e2e54702da0e", 1},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 1},
	{"dd2d03c19658ff96d371aef00e75e2e54702da0e", 2},
	{"ce9f123d790717599aaeb76bc62510de437761be", 2},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 1},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"b5c6053a46993b20d1b91e7b7206bffa54669ad7", 1},
	{"9e74d009894d73dd07773ea6b3bdd8323db980f7", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"d4b48a39aba7d3bd3e8abef2274a95b112d1ae73", 4},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 1},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 1},
	{"d2f6214b625db706384b378a29cc4c22237db97a", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 3},
	{"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941", 2},
	{"ce9f123d790717599aaeb76bc62510de437761be", 2},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 4},
	{"d2f6214b625db706384b378a29cc4c22237db97a", 1},
	{"b7015a5d36990d69a054482556127b9c7404a24a", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 5},
	{"b41d7c0e5b20bbe7c8eb6606731a3ff68f4e3941", 2},
	{"d2f6214b625db706384b378a29cc4c22237db97a", 1},
	{"ce9f123d790717599aaeb76bc62510de437761be", 5},
	{"ba486de7c025457963701114c683dcd4708e1dee", 4},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 1},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 3},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 1},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 3},
	{"6328ee836affafc1b52127147b5ca07300ac78e6", 2},
	{"01e65d67eed8afcb67a6bdf1c962541f62b299c9", 3},
	{"3de4f77c105f700f50d9549d32b9a05a01b46c4b", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 2},
	{"370d61cdbc1f3c90db6759f1599ccbabd40ad6c1", 6},
	{"dd7e66c862209e8b912694a582a09c0db3227f0d", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 2},
	{"dd7e66c862209e8b912694a582a09c0db3227f0d", 3},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 1},
	{"dd7e66c862209e8b912694a582a09c0db3227f0d", 1},
	{"8980daf661408a3faa1f22c225702a5c1d11d5c9", 3},
}
