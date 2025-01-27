package git

// Default supported transports.
import (
	_ "github.com/go-git/go-git/v6/plumbing/transport/file" // file transport
	_ "github.com/go-git/go-git/v6/plumbing/transport/git"  // git transport
	_ "github.com/go-git/go-git/v6/plumbing/transport/http" // http transport
	_ "github.com/go-git/go-git/v6/plumbing/transport/ssh"  // ssh transport
)
