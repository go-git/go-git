package git

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/tyba/oss/sources/vcs/clients/git/pktline"

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

func (c *Client) GetLastCommit() (string, error) {
	req, _ := c.buildRequest(
		"GET",
		fmt.Sprintf("%s/info/refs?service=git-upload-pack", c.url),
		nil,
	)

	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}

	if res.StatusCode >= 400 {
		return "", &NotFoundError{c.url}
	}

	defer res.Body.Close()
	d := pktline.NewDecoder(res.Body)

	content, err := d.ReadAll()
	if err != nil {
		return "", err
	}

	var head string
	for _, line := range content {
		if line[0] == '#' {
			continue
		}

		if head == "" {
			head = c.getHEADFromLine(line)
		} else {
			commit, branch := c.getCommitAndBranch(line)
			if branch == head {
				return commit, nil
			}
		}
	}

	return "", nil
}

func (c *Client) getHEADFromLine(line string) string {
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

func (c *Client) GetPackFile(want string) (io.ReadCloser, error) {
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

	req.Header.Add("User-Agent", "git/1.0")
	req.Header.Add("Host", "github.com")

	if content == nil {
		req.Header.Add("Accept", "*/*")
	} else {
		req.Header.Add("Accept", "application/x-git-upload-pack-result")
		req.Header.Add("Content-Type", "application/x-git-upload-pack-request")
		req.Header.Add("Content-Length", string(content.Len()))
	}

	return req, nil
}

type NotFoundError struct {
	url string
}

func (e NotFoundError) Error() string {
	return e.url
}
