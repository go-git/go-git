package url

import (
	"testing"
)

func TestMatchesScpLike(t *testing.T) {
	examples := []string{
		"git@github.com:james/bond",
		"git@github.com:007/bond",
		"git@github.com:22:james/bond",
		"git@github.com:22:007/bond",
	}

	for _, url := range examples {
		if !MatchesScpLike(url) {
			t.Fatalf("repo url %q did not match ScpLike", url)
		}
	}
}

func TestFindScpLikeComponents(t *testing.T) {
	url := "git@github.com:james/bond"
	user, host, port, path := FindScpLikeComponents(url)

	if user != "git" || host != "github.com" || port != "" || path != "james/bond" {
		t.Fatalf("repo url %q did not match properly", user)
	}

	url = "git@github.com:007/bond"
	user, host, port, path = FindScpLikeComponents(url)

	if user != "git" || host != "github.com" || port != "" || path != "007/bond" {
		t.Fatalf("repo url %q did not match properly", user)
	}

	url = "git@github.com:22:james/bond"
	user, host, port, path = FindScpLikeComponents(url)

	if user != "git" || host != "github.com" || port != "22" || path != "james/bond" {
		t.Fatalf("repo url %q did not match properly", user)
	}

	url = "git@github.com:22:007/bond"
	user, host, port, path = FindScpLikeComponents(url)

	if user != "git" || host != "github.com" || port != "22" || path != "007/bond" {
		t.Fatalf("repo url %q did not match properly", user)
	}
}
