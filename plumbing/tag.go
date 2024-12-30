package plumbing

// TagMode defines how the tags will be fetched from the remote repository.
type TagMode int

const (
	InvalidTagMode TagMode = iota
	// TagFollowing any tag that points into the histories being fetched is also
	// fetched. TagFollowing requires a server with `include-tag` capability
	// in order to fetch the annotated tags objects.
	TagFollowing
	// AllTags fetch all tags from the remote (i.e., fetch remote tags
	// refs/tags/* into local tags with the same name)
	AllTags
	// NoTags fetch no tags from the remote at all
	NoTags
)
