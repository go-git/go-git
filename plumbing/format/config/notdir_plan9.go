package config

// isNotDir reports whether err says that a component of a path is not a
// directory. Plan 9 has no ENOTDIR to test for.
func isNotDir(error) bool {
	return false
}
