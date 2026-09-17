package url

import "testing"

var benchURLs = []string{
	"https://github.com/go-git/go-git.git",
	"http://git:pass@github.com:8080/user/repository.git?foo#bar",
	"ssh://git@github.com:777/user/repository.git",
	"git://github.com/user/repository.git",
	"git@github.com:james/bond",
	"git@github.com:22:james/bond",
	"git@[fe80::1]:james/bond",
	"user@host.example.com:path/to/repo.git",
	"/home/user/src/go-git",
	"./relative/path/repo.git",
	"C:\\path\\to\\repo",
	"file:///path/to/repo",
	"foo.git",
}

var (
	sinkURL  any
	sinkBool bool
)

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		for _, u := range benchURLs {
			sinkURL, _ = Parse(u)
		}
	}
}

func BenchmarkMatchesScheme(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		for _, u := range benchURLs {
			sinkBool = MatchesScheme(u)
		}
	}
}

func BenchmarkMatchesScpLike(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		for _, u := range benchURLs {
			sinkBool = MatchesScpLike(u)
		}
	}
}

func BenchmarkIsLocalEndpoint(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		for _, u := range benchURLs {
			sinkBool = IsLocalEndpoint(u)
		}
	}
}
