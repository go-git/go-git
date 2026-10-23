// Package filemode provides types and utilities for working with git file modes.
package filemode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
)

// A FileMode represents the kind of tree entries used by git. It
// resembles regular file systems modes, although FileModes are
// considerably simpler (there are not so many), and there are some,
// like Submodule that has no file system equivalent.
type FileMode uint32

const (
	// Empty is used as the FileMode of tree elements when comparing
	// trees in the following situations:
	//
	// - the mode of tree elements before their creation.  - the mode of
	// tree elements after their deletion.  - the mode of unmerged
	// elements when checking the index.
	//
	// Empty has no file system equivalent.  As Empty is the zero value
	// of FileMode, it is also returned by New and
	// NewFromOsNewFromOSFileMode along with an error, when they fail.
	Empty FileMode = 0
	// Dir represent a Directory.
	Dir FileMode = 0o040000
	// Regular represent non-executable files.  Please note this is not
	// the same as golang regular files, which include executable files.
	Regular FileMode = 0o100644
	// Deprecated represent non-executable files with the group writable
	// bit set.  This mode was supported by the first versions of git,
	// but it has been deprecated nowadays.  This library uses them
	// internally, so you can read old packfiles, but will treat them as
	// Regulars when interfacing with the outside world.  This is the
	// standard git behaviour.
	Deprecated FileMode = 0o100664
	// Executable represents executable files.
	Executable FileMode = 0o100755
	// Symlink represents symbolic links to files.
	Symlink FileMode = 0o120000
	// Submodule represents git submodules.  This mode has no file system
	// equivalent.
	Submodule FileMode = 0o160000
)

// New takes the octal string representation of a FileMode and returns
// the FileMode and a nil error.  If the string can not be parsed to a
// 32 bit unsigned octal number, it returns Empty and the parsing error.
//
// Example: "40000" means Dir, "100644" means Regular.
//
// Please note this function does not check if the returned FileMode
// is valid in git or if it is malformed.  For instance, "1" will
// return the malformed FileMode(1) and a nil error.
func New(s string) (FileMode, error) {
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return Empty, err
	}

	return FileMode(n), nil
}

// FromBytes parses b, a run of octal digits such as the mode of a tree
// entry, as a FileMode. It accepts leading zeros, so zero-padded modes
// such as "040000", written by early Git versions, or
// "00000000000100644" parse. It avoids allocating a string when
// parsing from binary data.
//
// If b is empty, contains a byte other than '0' to '7', or holds a
// value wider than 32 bits, FromBytes returns [Empty] and an error, as
// [New] does.
//
// FromBytes does not check whether the result is a valid tree entry
// mode. Use [FileMode.Canonical] to normalize the result.
func FromBytes(b []byte) (FileMode, error) {
	if len(b) == 0 {
		return Empty, errors.New("empty mode")
	}

	var mode uint32
	for _, c := range b {
		if c < '0' || c > '7' {
			return Empty, fmt.Errorf("invalid octal character: %q", c)
		}
		if mode > math.MaxUint32>>3 {
			return Empty, errors.New("mode wider than 32 bits")
		}
		mode = mode<<3 | uint32(c-'0')
	}
	return FileMode(mode), nil
}

// NewFromOSFileMode returns the FileMode used by git to represent
// the provided file system modes and a nil error on success.  If the
// file system mode cannot be mapped to any valid git mode (as with
// sockets or named pipes), it will return Empty and an error.
//
// Note that some git modes cannot be generated from os.FileModes, like
// Deprecated and Submodule; while Empty will be returned, along with an
// error, only when the method fails.
func NewFromOSFileMode(m os.FileMode) (FileMode, error) {
	if m.IsRegular() {
		if isSetTemporary(m) {
			return Empty, fmt.Errorf("no equivalent git mode for %s", m)
		}
		if isSetCharDevice(m) {
			return Empty, fmt.Errorf("no equivalent git mode for %s", m)
		}
		if isSetUserExecutable(m) {
			return Executable, nil
		}
		return Regular, nil
	}

	if m.IsDir() {
		return Dir, nil
	}

	if isSetSymLink(m) {
		return Symlink, nil
	}

	return Empty, fmt.Errorf("no equivalent git mode for %s", m)
}

func isSetCharDevice(m os.FileMode) bool {
	return m&os.ModeCharDevice != 0
}

func isSetTemporary(m os.FileMode) bool {
	return m&os.ModeTemporary != 0
}

func isSetUserExecutable(m os.FileMode) bool {
	return m&0o100 != 0
}

func isSetSymLink(m os.FileMode) bool {
	return m&os.ModeSymlink != 0
}

// Bytes return a slice of 4 bytes with the mode in little endian
// encoding.
func (m FileMode) Bytes() []byte {
	ret := make([]byte, 4)
	binary.LittleEndian.PutUint32(ret, uint32(m))
	return ret
}

// IsMalformed returns if the FileMode should not appear in a git packfile,
// this is: Empty and any other mode not mentioned as a constant in this
// package.
func (m FileMode) IsMalformed() bool {
	return m != Dir &&
		m != Regular &&
		m != Deprecated &&
		m != Executable &&
		m != Symlink &&
		m != Submodule
}

// String returns the FileMode as a string in the standard git format,
// this is, an octal number padded with zeros to 7 digits.  Malformed
// modes are printed in that same format, for easier debugging.
//
// Example: Regular is "0100644", Empty is "0000000".
func (m FileMode) String() string {
	return fmt.Sprintf("%07o", uint32(m))
}

// IsRegular returns if the FileMode represents that of a regular file,
// this is, either Regular or Deprecated.  Please note that Executable
// are not regular even though in the UNIX tradition, they usually are:
// See the IsFile method.
func (m FileMode) IsRegular() bool {
	return m == Regular ||
		m == Deprecated
}

// IsFile returns if the FileMode represents that of a file, this is,
// Regular, Deprecated, Executable or Link.
func (m FileMode) IsFile() bool {
	return m == Regular ||
		m == Deprecated ||
		m == Executable ||
		m == Symlink
}

// Canonical returns the mode Git acts on for a tree entry stored with
// mode m, as Git's canon_mode[1] does. It is meant for modes read from
// tree objects: [Empty], which go-git elsewhere uses for an absent
// entry, becomes [Submodule] like any other unknown type.
//
// Only the file type bits (m & 0o170000) and the owner execute bit
// (0o100) count:
//
//   - type 0o100000 is [Executable] if the owner execute bit is set,
//     and [Regular] otherwise, so [Deprecated] and 0o100011 both
//     become Regular;
//   - type 0o120000 is [Symlink] and type 0o040000 is [Dir], whatever
//     their permission bits;
//   - any other type is [Submodule].
//
// The result is never malformed (see [FileMode.IsMalformed]), and
// Canonical of a canonical mode returns it unchanged.
//
// [1]: https://github.com/git/git/blob/v2.55.0/object.h#L133-L154
func (m FileMode) Canonical() FileMode {
	switch m & 0o170000 {
	case 0o100000:
		if m&0o100 != 0 {
			return Executable
		}
		return Regular
	case 0o120000:
		return Symlink
	case 0o040000:
		return Dir
	default:
		return Submodule
	}
}

// ToOSFileMode returns the os.FileMode to be used when creating file
// system elements with the given git mode and a nil error on success.
//
// When the provided mode cannot be mapped to a valid file system mode
// (e.g.  Submodule) it returns os.FileMode(0) and an error.
//
// The returned file mode does not take into account the umask.
func (m FileMode) ToOSFileMode() (os.FileMode, error) {
	switch m {
	case Dir:
		return os.ModePerm | os.ModeDir, nil
	case Submodule:
		return os.ModePerm | os.ModeDir, nil
	case Regular:
		return os.FileMode(0o644), nil
	// Deprecated is no longer allowed: treated as a Regular instead
	case Deprecated:
		return os.FileMode(0o644), nil
	case Executable:
		return os.FileMode(0o755), nil
	case Symlink:
		return os.ModePerm | os.ModeSymlink, nil
	}

	return os.FileMode(0), fmt.Errorf("malformed mode (%s)", m)
}
