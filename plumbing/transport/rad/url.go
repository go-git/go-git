package rad

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

// URL is the parsed form of a rad:// URL: rad://<rid>[/<nid>].
type URL struct {
	// RID is the Radicle repository id, e.g. "z2cK19PnX6cAUgnZfMfwECBNppJ6z".
	RID string
	// NID is the optional Radicle node id, selecting the namespaced
	// reference view (refs/namespaces/<NID>/*), e.g.
	// "z6Mkmywxg7Z7e63rkLTx7UJZeH69DLZ9KvhdQEPJ6N9nwVde". Empty selects the
	// canonical (non-namespaced) view.
	NID string
}

// parseURL parses u into a URL, accepting exactly the form rad://<rid>[/<nid>]
// — a host and at most one optional path segment — matching heartwood's
// Url::from_str. The host is used unmodified as the RID: base58 identifiers
// are case-sensitive, and url.Parse preserves Host case, so no case
// normalization happens here. Errors wrap transport.ErrInvalidRequest.
func parseURL(u *url.URL) (URL, error) {
	rid := u.Host
	if err := validateID(rid); err != nil {
		return URL{}, fmt.Errorf("%w: invalid rid %q: %s", transport.ErrInvalidRequest, rid, err)
	}

	nid := strings.Trim(u.Path, "/")
	if nid == "" {
		return URL{RID: rid}, nil
	}

	if strings.Contains(nid, "/") {
		return URL{}, fmt.Errorf("%w: too many path segments in %q", transport.ErrInvalidRequest, u.Path)
	}
	if err := validateID(nid); err != nil {
		return URL{}, fmt.Errorf("%w: invalid nid %q: %s", transport.ErrInvalidRequest, nid, err)
	}

	return URL{RID: rid, NID: nid}, nil
}

// validateID checks that id is safe to use as a single path segment: not
// empty, not "." or "..", and restricted to a base58btc-ish alphanumeric
// charset. This deliberately does not decode or verify base58 — its only
// job is to make path traversal through the resolved storage path
// impossible.
func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("empty identifier")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("identifier must not be %q", id)
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		default:
			return fmt.Errorf("identifier contains invalid character %q", r)
		}
	}
	return nil
}
