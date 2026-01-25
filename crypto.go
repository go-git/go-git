package git

import (
	// Register Go's SHA256 implementation.
	_ "crypto/sha256"
	// Register sha1cd implementation.
	_ "github.com/pjbgf/sha1cd"
)
