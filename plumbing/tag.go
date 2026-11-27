package plumbing

// TagMode defines how the tags will be fetched from the remote repository.
type TagMode int

// Tag modes for fetching tags.
const (
	InvalidTagMode TagMode = iota
	// TagFollowing any tag that points into the histories being fetched is also
	// fetched. Annotated tag objects the server does not send through the
	// `include-tag` capability are fetched in a second round.
	TagFollowing
	// AllTags fetch all tags from the remote (i.e., fetch remote tags
	// refs/tags/* into local tags with the same name)
	AllTags
	// NoTags fetch no tags from the remote at all
	NoTags
)
