package http

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// discovery carries what a discovery request is made of, so the request and any
// re-issue of it are built by the same code from the same values.
type discovery struct {
	service   string
	protocol  protocol.Version
	forceDumb bool
}

// query returns the discovery query, empty when the server is being treated as
// a dumb one.
func (d discovery) query() string {
	if d.forceDumb {
		return ""
	}
	return "service=" + d.service
}

// request builds the discovery GET for base.
//
// The URL is assembled from base's scheme, host and path alone — RawPath with
// it, so an escaped segment such as %2F survives — rather than from
// base.String(), which would carry base's userinfo into the request URL, where
// it reaches trace output and error strings, and would bring the clone URL's
// query and fragment along too. Authentication is applied by the caller, never
// from the URL.
func (d discovery) request(ctx context.Context, base *url.URL) (*http.Request, error) {
	origin := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: base.Path, RawPath: base.RawPath}
	infoURL := origin.JoinPath("info/refs").String()
	if q := d.query(); q != "" {
		infoURL += "?" + q
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("http transport: %w", err)
	}

	req.Header.Set("User-Agent", capability.DefaultAgent())
	if !d.forceDumb {
		if gp := transport.GitProtocolEnv(d.protocol); gp != "" {
			req.Header.Set("Git-Protocol", gp)
		}
	}
	return req, nil
}
