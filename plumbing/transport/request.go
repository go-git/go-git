package transport

import (
	"net/url"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// Request describes the operation to open on a remote.
//
// URL is the target remote. Command is the transport-neutral command name
// (e.g. "git-upload-pack", "git-receive-pack", "git-lfs-authenticate").
// Args carries additional arguments appended after the command and
// repository path. For example, git-lfs-authenticate produces
// `git-lfs-authenticate '<repo>' '<arg>'` on the wire.
// Protocol communicates the preferred Git wire protocol version.
//
// The repository path is not a field on Request. Adapters derive it from
// URL.Path, matching how canonical Git handles the relationship for all
// transport protocols.
//
// FromUser and Config feed the protocol-policy gate evaluated by
// CheckRequest. Config supplies protocol.<name>.allow and protocol.allow;
// when nil, only env-var policy and built-in defaults apply.
type Request struct {
	URL *url.URL

	Command string
	Args    []string

	Protocol protocol.Version

	// FromUser tracks whether this request was initiated directly by
	// the user. A non-nil value is taken at face value; a nil value
	// falls back to GIT_PROTOCOL_FROM_USER (default true), matching
	// canonical Git's transport_check_allowed.
	FromUser *bool
	Config   *config.Config
}
