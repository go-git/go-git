package transport

import "strings"

// Git service command names.
const (
	UploadPackService    = "git-upload-pack"
	UploadArchiveService = "git-upload-archive"
	ReceivePackService   = "git-receive-pack"
)

// ServiceName returns the service name without the "git-" prefix.
func ServiceName(service string) string {
	return strings.TrimPrefix(service, "git-")
}
