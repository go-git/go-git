package transport

// Service represents a Git transport service.
// All services are prefixed with "git-".
type Service string

// String returns the string representation of the service.
func (s Service) String() string {
	return string(s)
}

// Name returns the name of the service without the "git-" prefix.
func (s Service) Name() string {
	return string(s)[4:]
}

// Git service names.
const (
	UploadPackService    Service = "git-upload-pack"
	UploadArchiveService Service = "git-upload-archive"
	ReceivePackService   Service = "git-receive-pack"
)
