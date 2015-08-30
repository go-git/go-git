package git

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/tyba/srcd-crawler/clients/git/pktline"

	"github.com/sourcegraph/go-vcsurl"
)

type Client struct {
	url    string
	client *http.Client
}

func NewClient(url string) *Client {
	vcs, _ := vcsurl.Parse(url)
	return &Client{url: vcs.Link(), client: &http.Client{}}
}

func (c *Client) Refs() (*Refs, error) {
	req, _ := c.buildRequest(
		"GET",
		fmt.Sprintf("%s/info/refs?service=git-upload-pack", c.url),
		nil,
	)

	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode >= 400 {
		return nil, &NotFoundError{c.url}
	}

	defer res.Body.Close()
	d := pktline.NewDecoder(res.Body)

	content, err := d.ReadAll()
	if err != nil {
		return nil, err
	}

	return c.buildRefsFromContent(content), nil
}

func (c *Client) buildRefsFromContent(content []string) *Refs {
	refs := &Refs{branches: make(map[string]string, 0)}
	for _, line := range content {
		if line[0] == '#' {
			continue
		}

		if refs.defaultBranch == "" {
			refs.defaultBranch = c.getDefaultBranchFromLine(line)
		} else {
			commit, branch := c.getCommitAndBranch(line)
			refs.branches[branch] = commit
		}
	}

	return refs
}

func (c *Client) getDefaultBranchFromLine(line string) string {
	args, _ := url.ParseQuery(strings.Replace(line, " ", "&", -1))

	link, ok := args["symref"]
	if !ok {
		return ""
	}

	parts := strings.Split(link[0], ":")
	if len(parts) != 2 || parts[0] != "HEAD" {
		return ""
	}

	return parts[1]
}

func (c *Client) getCommitAndBranch(line string) (string, string) {
	parts := strings.Split(strings.Trim(line, " \n"), " ")
	if len(parts) != 2 {
		return "", ""
	}

	return parts[0], parts[1]
}

func (c *Client) PackFile(want string) (io.ReadCloser, error) {
	e := pktline.NewEncoder()
	e.AddLine(fmt.Sprintf("want %s", want))
	e.AddFlush()
	e.AddLine("done")

	req, err := c.buildRequest(
		"POST",
		fmt.Sprintf("%s/git-upload-pack", c.url),
		e.GetReader(),
	)
	if err != nil {
		return nil, err
	}

	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	h := make([]byte, 8)
	if _, err := res.Body.Read(h); err != nil {
		return nil, err
	}

	return res.Body, nil
}

func (c *Client) buildRequest(method, url string, content *strings.Reader) (*http.Request, error) {
	var req *http.Request
	var err error
	if content == nil {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, content)
	}

	if err != nil {
		return nil, err
	}

	c.applyHeadersToRequest(req, content)
	return req, nil
}

func (c *Client) applyHeadersToRequest(req *http.Request, content *strings.Reader) {
	req.Header.Add("User-Agent", "git/1.0")
	req.Header.Add("Host", "github.com")

	if content == nil {
		req.Header.Add("Accept", "*/*")
	} else {
		req.Header.Add("Accept", "application/x-git-upload-pack-result")
		req.Header.Add("Content-Type", "application/x-git-upload-pack-request")
		req.Header.Add("Content-Length", string(content.Len()))
	}
}

type NotFoundError struct {
	url string
}

func (e NotFoundError) Error() string {
	return e.url
}

type Refs struct {
	defaultBranch string
	branches      map[string]string
}

func (r *Refs) DefaultBranch() string {
	return r.defaultBranch
}

func (r *Refs) DefaultBranchCommit() string {
	return r.branches[r.defaultBranch]
}

func (r *Refs) Branches() map[string]string {
	return r.branches
}
