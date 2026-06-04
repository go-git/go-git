// Package reflog implements encoding and decoding of git reflog entries.
//
// Git stores reflog entries in .git/logs/<ref> files, one entry per line.
// Each line has the format:
//
//	<old-hash> <new-hash> <name> <<email>> <unix-timestamp> <timezone>\t<message>\n
//
// For example:
//
//	0000000000000000000000000000000000000000 abc1234... Author Name <author@example.com> 1234567890 +0000	commit (initial): Initial commit
package reflog
